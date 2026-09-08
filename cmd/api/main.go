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
	"github.com/Mirai3103/pos-cafe/internal/category"
	"github.com/Mirai3103/pos-cafe/internal/database"
	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/Mirai3103/pos-cafe/internal/eventbus"
	"github.com/Mirai3103/pos-cafe/internal/validator"
	_ "github.com/Mirai3103/pos-cafe/docs"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"
)

// @title POS Cafe API
// @version 1.0
// @description Backend API for Cafe Point of Sale System built with Vertical Slice Architecture and Idiomatic Go.
// @host localhost:8080
// @BasePath /api/v1
func main() {
	// 1. Structured Logging Setup (Idiomatic slog)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 2. Load Configuration
	cfg := config.Load()
	slog.Info("starting pos-cafe backend", "env", cfg.Environment, "port", cfg.Port)

	// 3. Database Initialization (PostgreSQL + Embedded Auto-Migrations)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect database", "error", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.Error("failed to close database", "error", closeErr)
		}
	}()

	// 4. Initialize Data Access Layer (sqlc)
	queries := sqlc.New(db)

	// 5. Initialize EventBus (Watermill In-Memory Pub/Sub)
	bus := eventbus.New(logger)
	defer func() {
		if closeErr := bus.Close(); closeErr != nil {
			slog.Error("failed to close eventbus", "error", closeErr)
		}
	}()

	// Demo Event Listener: Tự động lắng nghe sự kiện CategoryCreated
	_ = bus.Subscribe(context.Background(), category.TopicCategoryCreated, func(ctx context.Context, payload []byte) error {
		var evt category.CreatedEvent
		if err := json.Unmarshal(payload, &evt); err != nil {
			return err
		}
		slog.Info("📢 [EVENT CONSUMED]", "topic", category.TopicCategoryCreated, "category_id", evt.ID, "name", evt.Name)
		return nil
	})

	// 6. Setup Echo Web Server
	e := echo.New()
	e.HideBanner = true
	e.Validator = validator.New()

	// Essential Middlewares
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.CORS())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:   true,
		LogURI:      true,
		LogMethod:   true,
		LogLatency:  true,
		LogError:    true,
		HandleError: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
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

	// Health Check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// Swagger UI Documentation
	e.GET("/swagger/*", echoSwagger.WrapHandler)

	// 7. Register Vertical Slices
	v1 := e.Group("/api/v1")

	// Category Vertical Slices (Commands, Queries & Events)
	categorySlices := category.NewSlices(queries, bus)
	categorySlices.RegisterRoutes(v1)

	// 7. Start Server with Graceful Shutdown
	go func() {
		addr := fmt.Sprintf(":%s", cfg.Port)
		slog.Info("server listening", "address", addr)
		if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server startup failed", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal (SIGINT, SIGTERM)
	quitCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-quitCtx.Done()

	slog.Info("shutting down server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	} else {
		slog.Info("server exited properly")
	}
}
