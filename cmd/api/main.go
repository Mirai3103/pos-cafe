package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Mirai3103/pos-cafe/config"
	_ "github.com/Mirai3103/pos-cafe/docs"
	"github.com/Mirai3103/pos-cafe/internal/auth"
	"github.com/Mirai3103/pos-cafe/internal/category"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/eventbus"
	"github.com/Mirai3103/pos-cafe/internal/httpvalidator"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"
)

// @title POS Cafe API
// @version 1.0
// @description Backend API for Cafe Point of Sale System built with Vertical Slice Architecture and Idiomatic Go.
// @host localhost:8080
// @BasePath /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer " followed by token
func main() {
	// 1. Structured Logging Setup (Idiomatic slog)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 2. Intercept OS termination signals
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 3. Run application with graceful lifecycle handling
	if err := run(ctx, logger); err != nil {
		slog.Error("application terminated unexpectedly", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	// 1. Load and validate configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	slog.Info("starting pos-cafe backend", "env", cfg.Environment, "port", cfg.Port)

	// 2. Database Initialization (PostgreSQL + Embedded Auto-Migrations)
	dbCtx, dbCancel := context.WithTimeout(ctx, 15*time.Second)
	defer dbCancel()

	db, err := database.Open(dbCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.Error("failed to close database pool", "error", closeErr)
		}
	}()

	// 3. Initialize Data Access Layer (sqlc)
	queries := sqlc.New(db)

	// 4. Initialize EventBus (Watermill In-Memory Pub/Sub)
	bus := eventbus.New(logger)
	defer func() {
		if closeErr := bus.Close(); closeErr != nil {
			slog.Error("failed to close eventbus", "error", closeErr)
		}
	}()

	// Domain Event Listener: Log category events.
	//
	// Deliberately NOT the signal-aware ctx: that would kill this consumer the
	// instant SIGTERM lands, before e.Shutdown drains in-flight requests, so
	// events emitted by those requests would be silently dropped. The deferred
	// bus.Close above is the stop signal, and it waits for in-flight handlers.
	if err := bus.Subscribe(context.Background(), category.TopicCategoryCreated, func(_ context.Context, payload []byte) error {
		var evt category.CreatedEvent
		if err := json.Unmarshal(payload, &evt); err != nil {
			return err
		}
		slog.Info("📢 [EVENT CONSUMED]", "topic", category.TopicCategoryCreated, "category_id", evt.ID, "name", evt.Name)
		return nil
	}); err != nil {
		return fmt.Errorf("subscribe category.created event: %w", err)
	}

	// 5. Setup Echo Web Server & HTTP Timeouts
	e := echo.New()
	e.HideBanner = true
	e.Validator = httpvalidator.New()

	// Enforce HTTP connection timeouts on the underlying http.Server
	e.Server.ReadTimeout = cfg.ReadTimeout
	e.Server.WriteTimeout = cfg.WriteTimeout
	e.Server.IdleTimeout = cfg.IdleTimeout
	e.Server.ReadHeaderTimeout = cfg.ReadHeaderTimeout

	// Essential Middlewares
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: cfg.CORSAllowedOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:   true,
		LogURI:      true,
		LogMethod:   true,
		LogLatency:  true,
		LogError:    true,
		HandleError: true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error == nil {
				slog.Info("request",
					"method", v.Method,
					"uri", v.URI,
					"status", v.Status,
					"latency", v.Latency.String(),
				)
			} else {
				slog.Error("request error",
					"method", v.Method,
					"uri", v.URI,
					"status", v.Status,
					"latency", v.Latency.String(),
					"error", v.Error,
				)
			}
			return nil
		},
	}))

	// Health Check / Readiness Probe
	e.GET("/health", func(c echo.Context) error {
		pingCtx, pingCancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
		defer pingCancel()

		dbStatus := "up"
		if pingErr := db.PingContext(pingCtx); pingErr != nil {
			dbStatus = "down"
			return c.JSON(http.StatusServiceUnavailable, map[string]any{
				"status":   "unhealthy",
				"database": dbStatus,
				"error":    pingErr.Error(),
				"time":     time.Now().UTC().Format(time.RFC3339),
			})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"status":   "healthy",
			"database": dbStatus,
			"time":     time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Swagger UI Documentation
	e.GET("/swagger/*", echoSwagger.WrapHandler)

	// 6. Register Vertical Slices
	v1 := e.Group("/api/v1")
	authSlices := auth.NewSlices(db, queries)
	authSlices.RegisterRoutes(v1)

	categorySlices := category.NewSlices(queries, bus)
	categorySlices.RegisterRoutes(v1)

	// 7. Start Server with Graceful Shutdown error propagation
	serverErrChan := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf(":%s", cfg.Port)
		slog.Info("server listening", "address", addr)
		if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrChan <- err
		}
	}()

	// Wait for termination signal or server error
	select {
	case startErr := <-serverErrChan:
		return fmt.Errorf("server startup failed: %w", startErr)
	case <-ctx.Done():
		slog.Info("shutting down server gracefully...")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server graceful shutdown failed: %w", err)
	}

	slog.Info("server exited cleanly")
	return nil
}
