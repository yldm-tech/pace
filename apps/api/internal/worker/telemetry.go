package worker

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"gorm.io/gorm"
)

// PushInstanceMetricsTask reports how much an installation holds to whoever collects its telemetry. The beat runs it on an interval and registering the instance runs it once.
const PushInstanceMetricsTask = "plane.license.bgtasks.telemetry_metrics.push_instance_metrics"

// The three numbers the task is built around.
const (
	// workspaceMetricsLimit caps how many workspaces are reported one by one.
	workspaceMetricsLimit = 1000
	flushTimeout          = 30 * time.Second
	exportInterval        = 20 * time.Second
)

// There is no default endpoint on purpose. Metrics describe the instance and the workspaces inside it, and there is no address this project could pick that the operator did not choose: an unset OTLP_ENDPOINT means nothing is sent.
//
// This used to default to the upstream project's collector, so a deployment that configured nothing reported to a third party out of the box.

// TelemetryTasks pushes those numbers.
type TelemetryTasks struct {
	db     *gorm.DB
	webURL string
	logger *slog.Logger
}

func NewTelemetryTasks(db *gorm.DB, webURL string, logger *slog.Logger) *TelemetryTasks {
	return &TelemetryTasks{db: db, webURL: webURL, logger: logger}
}

func (tasks *TelemetryTasks) Register(consumer *Consumer) {
	consumer.Register(PushInstanceMetricsTask, tasks.pushInstanceMetrics)
}

// instanceRow is the registration this installation carries, and the thing that decides whether anything is reported at all.
type instanceRow struct {
	InstanceID         string `gorm:"column:instance_id"`
	InstanceName       string `gorm:"column:instance_name"`
	CurrentVersion     string `gorm:"column:current_version"`
	LatestVersion      string `gorm:"column:latest_version"`
	Edition            string `gorm:"column:edition"`
	IsVerified         bool   `gorm:"column:is_verified"`
	IsSetupDone        bool   `gorm:"column:is_setup_done"`
	IsTelemetryEnabled bool   `gorm:"column:is_telemetry_enabled"`
}

// pushInstanceMetrics reproduces push_instance_metrics.
//
// Two gates come before anything is counted: an installation that was never registered reports nothing, and one whose telemetry is turned off reports nothing. Everything after that is swallowed — the task logs and returns rather than failing, so a collector that is unreachable costs one log line and the next run tries again.
func (tasks *TelemetryTasks) pushInstanceMetrics(ctx context.Context, _ []any, _ map[string]any) error {
	var instance instanceRow
	err := tasks.db.WithContext(ctx).Table("instances").
		Select("instance_id, instance_name, current_version, latest_version, edition, is_verified, is_setup_done, is_telemetry_enabled").
		// Instance.Meta orders newest first, so .first() is the newest registration rather than the oldest.
		Where("deleted_at IS NULL").Order("created_at DESC").Limit(1).Take(&instance).Error
	if err != nil {
		tasks.logger.Debug("no instance registered, skipping metrics push")
		return nil
	}
	if !instance.IsTelemetryEnabled {
		tasks.logger.Debug("telemetry disabled, skipping metrics push")
		return nil
	}
	// Nowhere to send it. An instance reports only to a collector its operator named.
	if os.Getenv("OTLP_ENDPOINT") == "" {
		tasks.logger.Debug("OTLP_ENDPOINT is not set, skipping metrics push")
		return nil
	}

	exporter, endpoint, err := newOTLPMetricExporter(ctx)
	if err != nil {
		tasks.logger.Warn("the telemetry exporter could not be built", "error", err)
		return nil
	}
	tasks.logger.Info("configuring the OTLP exporter", "endpoint", endpoint)

	resources, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", envOrFallback("SERVICE_NAME", "plane-ce-api")),
		attribute.String("instance_id", instance.InstanceID),
		attribute.String("plane.instance.type", "self-hosted"),
	))
	if err != nil {
		tasks.logger.Warn("the telemetry resource could not be built", "error", err)
		return nil
	}
	reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(exportInterval))
	// A fresh provider per run, because the gauges read counts captured when they are registered.
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithResource(resources), sdkmetric.WithReader(reader))
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
		defer cancel()
		_ = provider.Shutdown(shutdown)
	}()

	if err := tasks.registerGauges(ctx, provider.Meter("plane.license.bgtasks.telemetry_metrics"), instance); err != nil {
		tasks.logger.Warn("the telemetry counts could not be read", "error", err)
		return nil
	}

	flush, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	if err := provider.ForceFlush(flush); err != nil {
		tasks.logger.Warn("the metrics flush timed out, some metrics may not have been exported",
			"instance", instance.InstanceID, "error", err)
		return nil
	}
	tasks.logger.Info("pushed metrics to the OTEL collector", "endpoint", endpoint, "instance", instance.InstanceID)
	return nil
}

// instanceGauges are the nine numbers reported for the installation as a whole, with the SQL each is counted by.
var instanceGauges = []struct {
	Name        string
	Description string
	Table       string
	Where       string
}{
	{"plane_instance_users_total", "Total number of users in the Plane instance", "users", "is_bot = FALSE"},
	{"plane_instance_workspaces_total", "Total number of workspaces", "workspaces", "deleted_at IS NULL"},
	{"plane_instance_projects_total", "Total number of projects across all workspaces", "projects", "deleted_at IS NULL"},
	{"plane_instance_issues_total", "Total number of issues across all projects", "issues", "deleted_at IS NULL"},
	{"plane_instance_modules_total", "Total number of modules", "modules", "deleted_at IS NULL"},
	{"plane_instance_cycles_total", "Total number of cycles", "cycles", "deleted_at IS NULL"},
	{"plane_instance_cycle_issues_total", "Total number of issues in cycles", "cycle_issues", "deleted_at IS NULL"},
	{"plane_instance_module_issues_total", "Total number of issues in modules", "module_issues", "deleted_at IS NULL"},
}

// registerGauges reads every count and hangs it off a gauge, which the reader then exports.
func (tasks *TelemetryTasks) registerGauges(ctx context.Context, meter metric.Meter, instance instanceRow) error {
	attributes := instanceAttributes(instance, telemetryDomain(tasks.webURL))

	for _, gauge := range instanceGauges {
		var count int64
		if err := tasks.db.WithContext(ctx).Table(gauge.Table).Where(gauge.Where).Count(&count).Error; err != nil {
			return err
		}
		observed := count
		_, err := meter.Int64ObservableGauge(gauge.Name,
			metric.WithDescription(gauge.Description),
			metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
				observer.Observe(observed, metric.WithAttributes(attributes...))
				return nil
			}))
		if err != nil {
			return err
		}
	}

	// The page count is the one that is not a plain count. It leaves out pages that are *both* owned by a bot and private — one condition rather than two, so a bot's public page is counted and so is a person's private one. Django renders it as an inner join and a negated pair, which is what this is.
	var pageCount int64
	err := tasks.db.WithContext(ctx).Table("pages p").
		Joins("INNER JOIN users u ON p.owned_by_id = u.id").
		Where("p.deleted_at IS NULL AND NOT (p.access = 1 AND u.is_bot)").
		Count(&pageCount).Error
	if err != nil {
		return err
	}
	_, err = meter.Int64ObservableGauge("plane_instance_pages_total",
		metric.WithDescription("Total number of pages"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			observer.Observe(pageCount, metric.WithAttributes(attributes...))
			return nil
		}))
	if err != nil {
		return err
	}

	return tasks.registerWorkspaceGauges(ctx, meter, instance.InstanceID)
}

// workspaceMetric is one workspace's six numbers.
type workspaceMetric struct {
	ID           string `gorm:"column:id"`
	Slug         string `gorm:"column:slug"`
	ProjectCount int64  `gorm:"column:project_count"`
	IssueCount   int64  `gorm:"column:issue_count"`
	ModuleCount  int64  `gorm:"column:module_count"`
	CycleCount   int64  `gorm:"column:cycle_count"`
	MemberCount  int64  `gorm:"column:member_count"`
	PageCount    int64  `gorm:"column:page_count"`
}

// workspaceGauges are the six reported per workspace, paired with the field each reads.
var workspaceGauges = []struct {
	Name        string
	Description string
	Value       func(workspaceMetric) int64
}{
	{"plane_workspace_projects_total", "Number of projects per workspace", func(row workspaceMetric) int64 { return row.ProjectCount }},
	{"plane_workspace_issues_total", "Number of issues per workspace", func(row workspaceMetric) int64 { return row.IssueCount }},
	{"plane_workspace_modules_total", "Number of modules per workspace", func(row workspaceMetric) int64 { return row.ModuleCount }},
	{"plane_workspace_cycles_total", "Number of cycles per workspace", func(row workspaceMetric) int64 { return row.CycleCount }},
	{"plane_workspace_members_total", "Number of members per workspace", func(row workspaceMetric) int64 { return row.MemberCount }},
	{"plane_workspace_pages_total", "Number of pages per workspace", func(row workspaceMetric) int64 { return row.PageCount }},
}

// registerWorkspaceGauges reports the oldest thousand workspaces one by one.
//
// The counts are read in one statement rather than six per workspace, which is what the batched aggregation upstream is for. A workspace with none of something is absent from its aggregation and reported as zero rather than left out.
func (tasks *TelemetryTasks) registerWorkspaceGauges(ctx context.Context, meter metric.Meter, instanceID string) error {
	var rows []workspaceMetric
	err := tasks.db.WithContext(ctx).Table("workspaces w").
		Select(`w.id, w.slug,
			(SELECT COUNT(*) FROM projects p WHERE p.workspace_id = w.id AND p.deleted_at IS NULL) AS project_count,
			(SELECT COUNT(*) FROM issues i WHERE i.workspace_id = w.id AND i.deleted_at IS NULL) AS issue_count,
			(SELECT COUNT(*) FROM modules m WHERE m.workspace_id = w.id AND m.deleted_at IS NULL) AS module_count,
			(SELECT COUNT(*) FROM cycles c WHERE c.workspace_id = w.id AND c.deleted_at IS NULL) AS cycle_count,
			(SELECT COUNT(*) FROM workspace_members wm WHERE wm.workspace_id = w.id AND wm.deleted_at IS NULL) AS member_count,
			(SELECT COUNT(*) FROM pages pg INNER JOIN users pu ON pg.owned_by_id = pu.id
				WHERE pg.workspace_id = w.id AND pg.deleted_at IS NULL
				AND NOT (pg.access = 1 AND pu.is_bot)) AS page_count`).
		Where("w.deleted_at IS NULL").
		// A deterministic order, so the thousand reported is the same thousand from one run to the next.
		Order("w.created_at").Limit(workspaceMetricsLimit).Scan(&rows).Error
	if err != nil {
		return err
	}

	for _, gauge := range workspaceGauges {
		value := gauge.Value
		observed := rows
		_, err := meter.Int64ObservableGauge(gauge.Name,
			metric.WithDescription(gauge.Description),
			metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
				for _, row := range observed {
					observer.Observe(value(row), metric.WithAttributes(
						attribute.String("workspace_id", row.ID),
						attribute.String("workspace_slug", row.Slug),
						attribute.String("instance_id", instanceID),
					))
				}
				return nil
			}))
		if err != nil {
			return err
		}
	}
	return nil
}

// instanceAttributes are the labels every instance-level number carries. The two booleans are rendered lowercase, which is what python's str() then .lower() leaves.
func instanceAttributes(instance instanceRow, domain string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("instance_id", instance.InstanceID),
		attribute.String("instance_name", instance.InstanceName),
		attribute.String("current_version", instance.CurrentVersion),
		attribute.String("latest_version", instance.LatestVersion),
		attribute.String("edition", instance.Edition),
		attribute.String("domain", domain),
		attribute.String("is_verified", strconv.FormatBool(instance.IsVerified)),
		attribute.String("is_setup_done", strconv.FormatBool(instance.IsSetupDone)),
	}
}

// telemetryDomain is the host WEB_URL names, with a scheme-less value read as a host rather than as a path.
func telemetryDomain(webURL string) string {
	if webURL == "" {
		return ""
	}
	if !strings.Contains(webURL, "://") {
		webURL = "//" + webURL
	}
	parsed, err := url.Parse(webURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

// newOTLPMetricExporter builds the exporter the protocol asks for, which is gRPC unless something says http.
func newOTLPMetricExporter(ctx context.Context) (sdkmetric.Exporter, string, error) {
	protocol := strings.ToLower(strings.TrimSpace(os.Getenv("OTLP_METRICS_PROTOCOL")))
	if protocol == "" {
		protocol = "grpc"
	}
	if protocol == "grpc" {
		endpoint := otlpGRPCEndpoint()
		options := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(endpoint)}
		if strings.EqualFold(os.Getenv("OTEL_EXPORTER_OTLP_METRICS_INSECURE"), "true") {
			options = append(options, otlpmetricgrpc.WithInsecure())
		}
		exporter, err := otlpmetricgrpc.New(ctx, options...)
		return exporter, endpoint, err
	}
	endpoint := otlpHTTPMetricsURL()
	exporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	return exporter, endpoint, err
}

// The two ports a gRPC endpoint falls back to when the url names none: an https url reaches an ingress on 443, and anything else reaches a collector on its own port.
const (
	otlpGRPCDefaultPort = "4317"
	httpsDefaultPort    = "443"
)

// otlpGRPCEndpoint is grpc_endpoint_from_url: the host and port to dial, derived from the one url both metrics and traces are configured with.
func otlpGRPCEndpoint() string {
	raw := os.Getenv("OTLP_ENDPOINT")
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host == "" {
		return ""
	}
	if port := parsed.Port(); port != "" {
		return host + ":" + port
	}
	if parsed.Scheme == "https" {
		return host + ":" + httpsDefaultPort
	}
	return host + ":" + otlpGRPCDefaultPort
}

// otlpHTTPMetricsURL is get_otlp_http_metrics_url: the same url with the metrics path on the end.
func otlpHTTPMetricsURL() string {
	endpoint := os.Getenv("OTLP_ENDPOINT")
	if endpoint == "" {
		return ""
	}
	return strings.TrimRight(endpoint, "/") + "/v1/metrics"
}

func envOrFallback(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
