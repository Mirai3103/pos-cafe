package testdb

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var allowedPackages = map[string]struct{}{
	"auth": {}, "catalog": {}, "database": {}, "preparation": {},
	"sales": {}, "shift": {}, "tables": {},
}

var (
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	suffixPattern     = regexp.MustCompile(`^[0-9a-f]{12}$`)
)

// overriddenDatabaseParams are pgx connection parameters that would override
// the database name parsed from the URL path, defeating the mandatory _test
// safety check. `service` is rejected too because a service-file entry can
// override the database and every other parameter.
var overriddenDatabaseParams = map[string]struct{}{
	"dbname":   {},
	"database": {},
	"service":  {},
}

// sensitiveQueryParams carry credentials and must be redacted from any URL
// that reaches a log or error.
var sensitiveQueryParams = map[string]struct{}{
	"password":    {},
	"sslpassword": {},
}

type config struct {
	baseConfig
	cloneName string
	cloneDSN  string
}

type baseConfig struct {
	baseURL        *url.URL
	baseName       string
	templateName   string
	maintenanceDSN string
	templateDSN    string
}

func parseBaseConfig(rawURL string) (baseConfig, error) {
	if rawURL == "" {
		return baseConfig{}, errors.New("TEST_DATABASE_URL is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return baseConfig{}, errors.New("TEST_DATABASE_URL is not a valid postgres URL")
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return baseConfig{}, errors.New("TEST_DATABASE_URL must be a postgres URL")
	}
	if parsed.Host == "" {
		return baseConfig{}, errors.New("TEST_DATABASE_URL host is required")
	}
	for key := range parsed.Query() {
		if _, overrides := overriddenDatabaseParams[strings.ToLower(key)]; overrides {
			return baseConfig{}, errors.New("TEST_DATABASE_URL must not override the database via query parameters")
		}
	}
	escapedName := strings.TrimPrefix(parsed.EscapedPath(), "/")
	if escapedName == "" || strings.Contains(escapedName, "/") {
		return baseConfig{}, errors.New("TEST_DATABASE_URL must contain one database name")
	}
	baseName, err := url.PathUnescape(escapedName)
	if err != nil {
		return baseConfig{}, errors.New("TEST_DATABASE_URL contains an invalid database name")
	}
	if !identifierPattern.MatchString(baseName) {
		return baseConfig{}, errors.New("TEST_DATABASE_URL database name is not a safe test identifier")
	}
	if !strings.HasSuffix(baseName, "_test") {
		return baseConfig{}, errors.New("TEST_DATABASE_URL database name must end in _test")
	}
	templateName := baseName + "_template"
	if len(templateName) > 63 {
		return baseConfig{}, errors.New("template database name exceeds 63 bytes")
	}
	return baseConfig{
		baseURL:        parsed,
		baseName:       baseName,
		templateName:   templateName,
		maintenanceDSN: databaseDSN(parsed, "postgres"),
		templateDSN:    databaseDSN(parsed, templateName),
	}, nil
}

func parseConfig(rawURL, packageName, suffix string) (config, error) {
	base, err := parseBaseConfig(rawURL)
	if err != nil {
		return config{}, err
	}
	if _, ok := allowedPackages[packageName]; !ok {
		return config{}, fmt.Errorf("unsupported integration package %q", packageName)
	}
	if !suffixPattern.MatchString(suffix) {
		return config{}, errors.New("invalid clone suffix")
	}
	cloneName := base.baseName + "_" + packageName + "_" + suffix
	if len(cloneName) > 63 {
		return config{}, errors.New("clone database name exceeds 63 bytes")
	}
	if !identifierPattern.MatchString(cloneName) {
		return config{}, errors.New("clone database name is not a safe identifier")
	}
	return config{
		baseConfig: base,
		cloneName:  cloneName,
		cloneDSN:   databaseDSN(base.baseURL, cloneName),
	}, nil
}

func databaseDSN(base *url.URL, databaseName string) string {
	dst := *base
	dst.Path = "/" + databaseName
	dst.RawPath = ""
	return dst.String()
}

func newSuffix() (string, error) {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate clone suffix: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func isEphemeralClone(baseName, databaseName string) bool {
	for packageName := range allowedPackages {
		prefix := baseName + "_" + packageName + "_"
		if strings.HasPrefix(databaseName, prefix) && suffixPattern.MatchString(strings.TrimPrefix(databaseName, prefix)) {
			return true
		}
	}
	return false
}

func sanitizeError(err error, rawURL string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	parsed, parseErr := url.Parse(rawURL)
	if parseErr != nil {
		return errors.New("integration database operation failed")
	}
	message = strings.ReplaceAll(message, rawURL, redactURL(parsed))
	secrets := secretValues(parsed)
	for _, value := range secrets {
		for _, variant := range []string{value, url.QueryEscape(value), url.PathEscape(value)} {
			message = strings.ReplaceAll(message, variant, "xxxxx")
		}
	}
	return errors.New(message)
}

// redactURL renders a URL safe for logs and errors: the userinfo password is
// masked and every sensitive query parameter is replaced, so credentials
// passed in either position cannot leak.
func redactURL(u *url.URL) string {
	redacted := *u
	if redacted.User != nil {
		redacted.User = url.UserPassword(redacted.User.Username(), "xxxxx")
	}
	query := redacted.Query()
	for key, values := range query {
		if _, sensitive := sensitiveQueryParams[strings.ToLower(key)]; !sensitive {
			continue
		}
		redactedValues := make([]string, len(values))
		for i := range values {
			redactedValues[i] = "xxxxx"
		}
		query[key] = redactedValues
	}
	redacted.RawQuery = query.Encode()
	return redacted.String()
}

// secretValues collects every credential embedded in the URL — the userinfo
// password and each sensitive query parameter value — so error text that
// embeds them in escaped form is covered too.
func secretValues(u *url.URL) []string {
	var secrets []string
	if u.User != nil {
		if password, ok := u.User.Password(); ok && password != "" {
			secrets = append(secrets, password)
		}
	}
	for key, values := range u.Query() {
		if _, sensitive := sensitiveQueryParams[strings.ToLower(key)]; !sensitive {
			continue
		}
		secrets = append(secrets, values...)
	}
	return secrets
}
