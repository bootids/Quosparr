package export

import (
	"time"

	"github.com/licoy/quosparr/internal/apiquota"
	"github.com/licoy/quosparr/internal/estimate"
	"github.com/licoy/quosparr/internal/sessions"
)

// PanelSchemaVersion is the current schema of the panel block.
const PanelSchemaVersion = 1

// StatusPanel is the extended read-only panel mirror of the status file: the
// derived data the remote view needs to render the main panel like a local
// install. It never carries credentials, emails or tokens, and account labels
// are resolved without the email fallback. A missing block (an older source
// machine) degrades the remote view to the widget-level summary.
type StatusPanel struct {
	Schema int                 `json:"schema"`
	Codex  *CodexPanelBlock    `json:"codex,omitempty"`
	Grok   *ProviderPanelBlock `json:"grok,omitempty"`
	Claude *ProviderPanelBlock `json:"claude,omitempty"`
	// Meters holds the quota-only providers (cursor, kimi, zai, agy) keyed
	// by their provider string; absent when none has data.
	Meters map[string]MeterPanelBlock `json:"meters,omitempty"`
}

// MeterPanelBlock mirrors a quota-only provider for the remote panel: the
// availability, the plan, the same quota rows the widget snapshot carries,
// the detail lines, the per-pool rate analysis and the last fetch time.
// Error is the already-redacted user-facing failure text, empty when the
// last fetch succeeded. Rates is optional: files from older producers
// decode without it (schema stays 1).
type MeterPanelBlock struct {
	Availability string            `json:"availability"`
	Plan         string            `json:"plan,omitempty"`
	Meters       []WidgetQuota     `json:"meters,omitempty"`
	Detail       []PanelDetailLine `json:"detail,omitempty"`
	// BalanceText is the compact prepaid-balance summary of a provider with
	// no quota pools; the remote floating window shows it as its status.
	BalanceText string            `json:"balanceText,omitempty"`
	Rates       []MeterPanelRate  `json:"rates,omitempty"`
	Speed       *PanelSpeedReport `json:"speed,omitempty"`
	FetchedAt   *time.Time        `json:"fetchedAt,omitempty"`
	Error       string            `json:"error,omitempty"`
}

// MeterPanelRate mirrors one pool's consumption-rate analysis (the runway
// MeterRate). Times marshal as RFC3339 like every other panel timestamp and
// are zero when their Has* flag is false. Derived numbers and plain titles
// only, never credentials.
type MeterPanelRate struct {
	ID         string    `json:"id"`
	Title      string    `json:"title,omitempty"`
	Unit       string    `json:"unit,omitempty"`
	IsPercent  bool      `json:"isPercent,omitempty"`
	PerHour    float64   `json:"perHour,omitempty"`
	HasRate    bool      `json:"hasRate,omitempty"`
	Samples    int       `json:"samples,omitempty"`
	From       time.Time `json:"from,omitempty"`
	To         time.Time `json:"to,omitempty"`
	ExhaustsAt time.Time `json:"exhaustsAt,omitempty"`
	HasExhaust bool      `json:"hasExhaust,omitempty"`
	ResetsAt   time.Time `json:"resetsAt,omitempty"`
	HasReset   bool      `json:"hasReset,omitempty"`
	AtReset    float64   `json:"atReset,omitempty"`
	HasAtReset bool      `json:"hasAtReset,omitempty"`
}

// PanelDetailLine is one "title: value" row of a remote panel section.
type PanelDetailLine struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

// CodexPanelBlock extends the Codex mirror beyond the widget summary.
type CodexPanelBlock struct {
	Estimate *PanelEstimate `json:"estimate,omitempty"`
	// AllDevicesTokens and LocalTokens are both chart series so the remote
	// chart keeps its device switch; TokensAsOf and TokensGeneratedAt mirror
	// HeatmapData.OfficialStatsAsOf / OfficialGeneratedAt.
	AllDevicesTokens []DailyTokens `json:"allDevicesTokens,omitempty"`
	LocalTokens      []DailyTokens `json:"localTokens,omitempty"`
	TokensAsOf       *time.Time    `json:"tokensAsOf,omitempty"`
	TokensGenerated  *time.Time    `json:"tokensGenerated,omitempty"`
	// ResetCredits is the full credit list behind the widget counts.
	ResetCredits *PanelResetCredits `json:"resetCredits,omitempty"`
	// Speed mirrors the model speed report.
	Speed *PanelSpeedReport `json:"speed,omitempty"`
	// APIKeyMode mirrors the relay / plain API key login and its balance.
	APIKeyMode bool            `json:"apiKeyMode,omitempty"`
	APIBaseURL string          `json:"apiBaseUrl,omitempty"`
	APIBalance *apiquota.Quota `json:"apiBalance,omitempty"`
	// Accounts is the read-only managed account list; ActiveAccountID marks
	// the current one and Display the signed-in identity (no email).
	Accounts        []PanelAccount `json:"accounts,omitempty"`
	ActiveAccountID string         `json:"activeAccountId,omitempty"`
	Display         *PanelIdentity `json:"display,omitempty"`
}

// ProviderPanelBlock extends the Grok / Claude mirror beyond the widget
// summary. The Claude-only and Grok-only fields stay absent on the other
// provider.
type ProviderPanelBlock struct {
	// Cost mirrors the API-equivalent cost summary of the current cycle,
	// daily rows included (the remote cost detail filters them per range).
	Cost *CostSummary `json:"cost,omitempty"`
	// LocalTokens is the provider's local chart series.
	LocalTokens []DailyTokens `json:"localTokens,omitempty"`
	// RecentSessions mirrors the provider's recent session list.
	RecentSessions []sessions.Item `json:"recentSessions,omitempty"`
	// Speed mirrors the model speed report.
	Speed *PanelSpeedReport `json:"speed,omitempty"`
	// ResetCredits is the credit list behind the shared reset display.
	ResetCredits *PanelResetCredits `json:"resetCredits,omitempty"`
	// Accounts is the read-only managed account list; AccountQuotas are the
	// cached per-account lines (Title is rebuilt locally from Plan/Period).
	Accounts        []PanelAccount      `json:"accounts,omitempty"`
	ActiveAccountID string              `json:"activeAccountId,omitempty"`
	Display         *PanelIdentity      `json:"display,omitempty"`
	AccountQuotas   []PanelAccountQuota `json:"accountQuotas,omitempty"`
	// ExtraUsage is Claude's raw extra-usage line.
	ExtraUsage string `json:"extraUsage,omitempty"`
	// API mirrors a Claude API relay account view.
	API *PanelAPIView `json:"api,omitempty"`
	// Billing mirrors Grok's billing details popover.
	Billing *PanelGrokBilling `json:"billing,omitempty"`
}

// PanelIdentity is the signed-in identity without the email.
type PanelIdentity struct {
	Authenticated bool   `json:"authenticated,omitempty"`
	DisplayName   string `json:"displayName,omitempty"`
	// Tier is the codex plan tier; it drives the plan badge colors.
	Tier string `json:"tier,omitempty"`
	// SubscriptionExpiresAt is the billing period end of the plan.
	SubscriptionExpiresAt *time.Time `json:"subscriptionExpiresAt,omitempty"`
}

// PanelAccount mirrors a managed account row for the read-only remote list.
// It never carries the email; Label is the source machine's resolved display
// name (alias, display name, username or a shortened id, never an email).
type PanelAccount struct {
	ID                    string                 `json:"id"`
	Label                 string                 `json:"label,omitempty"`
	SortIndex             int                    `json:"sortIndex,omitempty"`
	AuthMode              string                 `json:"authMode,omitempty"` // oauth | apikey
	Username              string                 `json:"username,omitempty"`
	UserID                string                 `json:"userId,omitempty"`
	SubjectID             string                 `json:"subjectId,omitempty"`
	AccountID             string                 `json:"accountId,omitempty"`
	WorkspaceName         string                 `json:"workspaceName,omitempty"`
	DisplayName           string                 `json:"displayName,omitempty"`
	Alias                 string                 `json:"alias,omitempty"`
	Note                  string                 `json:"note,omitempty"`
	PlanType              string                 `json:"planType,omitempty"`
	SubscriptionExpiresAt *time.Time             `json:"subscriptionExpiresAt,omitempty"`
	CreatedAt             time.Time              `json:"createdAt,omitempty"`
	LastUsedAt            *time.Time             `json:"lastUsedAt,omitempty"`
	LastQuotaAt           *time.Time             `json:"lastQuotaAt,omitempty"`
	RequiresReauth        bool                   `json:"requiresReauth,omitempty"`
	APIBaseURL            string                 `json:"apiBaseUrl,omitempty"`
	APIModel              string                 `json:"apiModel,omitempty"`
	APIQuota              *apiquota.Quota        `json:"apiQuota,omitempty"`
	Quota                 *PanelAccountQuotaLine `json:"quota,omitempty"`
}

// PanelAccountQuotaLine mirrors the Codex cached quota of an account row.
type PanelAccountQuotaLine struct {
	Plan                   string               `json:"plan,omitempty"`
	PrimaryUsedPercent     int                  `json:"primaryUsedPercent,omitempty"`
	PrimaryResetsAt        *time.Time           `json:"primaryResetsAt,omitempty"`
	PrimaryWindowMinutes   *int                 `json:"primaryWindowMinutes,omitempty"`
	SecondaryUsedPercent   *int                 `json:"secondaryUsedPercent,omitempty"`
	SecondaryResetsAt      *time.Time           `json:"secondaryResetsAt,omitempty"`
	SecondaryWindowMinutes *int                 `json:"secondaryWindowMinutes,omitempty"`
	AdditionalMeters       []PanelQuotaMeterRow `json:"additionalMeters,omitempty"`
	UpdatedAt              time.Time            `json:"updatedAt,omitempty"`
}

// PanelQuotaMeterRow is one additional cached meter of an account row.
type PanelQuotaMeterRow struct {
	ID            string `json:"id"`
	WindowMinutes *int   `json:"windowMinutes,omitempty"`
	Name          string `json:"name,omitempty"`
	UsedPercent   int    `json:"usedPercent,omitempty"`
}

// PanelAccountQuota is the cached quota line of one Grok / Claude account.
// Title is rebuilt on the reader from Plan and PeriodKind, so no localized
// string travels in the file.
type PanelAccountQuota struct {
	ID          string     `json:"id"`
	Plan        string     `json:"plan,omitempty"`
	PeriodKind  string     `json:"periodKind,omitempty"` // weekly | monthly
	UsedPercent int        `json:"usedPercent,omitempty"`
	ResetsAt    *time.Time `json:"resetsAt,omitempty"`
	IncludedUSD string     `json:"includedUsd,omitempty"` // Grok "$used / $limit"
	ExtraUsage  string     `json:"extraUsage,omitempty"`  // Claude raw line
	Balance     string     `json:"balance,omitempty"`     // Claude API relay
	UpdatedAt   time.Time  `json:"updatedAt,omitempty"`
}

// PanelEstimate mirrors estimate.Snapshot (derived numbers only).
type PanelEstimate struct {
	WindowMode        string                   `json:"windowMode"`
	CycleStartDate    string                   `json:"cycleStartDate"`
	UsedPercent       float64                  `json:"usedPercent"`
	UsedCredits       float64                  `json:"usedCredits"`
	UsedUSD           float64                  `json:"usedUsd"`
	DayCount          int                      `json:"dayCount"`
	EstimatedCredits  *float64                 `json:"estimatedCredits,omitempty"`
	EstimatedUSD      *float64                 `json:"estimatedUsd,omitempty"`
	CanExtrapolate    bool                     `json:"canExtrapolate,omitempty"`
	HasWeeklyWindow   bool                     `json:"hasWeeklyWindow,omitempty"`
	CurrentRows       []PanelEstimateRow       `json:"currentRows,omitempty"`
	PreviousEstimate  *estimate.HistorySample  `json:"previousEstimate,omitempty"`
	ChangePercent     *float64                 `json:"changePercent,omitempty"`
	ChangeKind        string                   `json:"changeKind,omitempty"`
	History           []estimate.HistorySample `json:"history,omitempty"`
	CalculatedAt      time.Time                `json:"calculatedAt"`
	UnavailableReason string                   `json:"unavailableReason,omitempty"`
	// Error mirrors CodexView.EstimateError, "" when fine.
	Error string `json:"error,omitempty"`
}

// PanelEstimateRow is one day of the estimate table.
type PanelEstimateRow struct {
	Date            string   `json:"date"`
	Credits         float64  `json:"credits"`
	Tokens          int      `json:"tokens"`
	Turns           int      `json:"turns"`
	USD             *float64 `json:"usd,omitempty"`
	CreditsReported bool     `json:"creditsReported,omitempty"`
	TotalsReported  bool     `json:"totalsReported,omitempty"`
}

// PanelResetCredits mirrors a reset credit snapshot (counts and list).
type PanelResetCredits struct {
	AvailableCount int                `json:"availableCount"`
	Credits        []PanelResetCredit `json:"credits,omitempty"`
	UpdatedAt      time.Time          `json:"updatedAt,omitempty"`
}

// PanelResetCredit is one reset credit; IDs are opaque strings used for the
// detail list and alert dedupe, never credentials.
type PanelResetCredit struct {
	ID               string    `json:"id"`
	Status           string    `json:"status,omitempty"`
	CreatedAt        time.Time `json:"createdAt,omitempty"`
	ExpiresAt        time.Time `json:"expiresAt,omitempty"`
	RemainingSeconds float64   `json:"remainingSeconds,omitempty"`
}

// PanelSpeedReport mirrors speed.Report.
type PanelSpeedReport struct {
	Groups       []PanelSpeedStat    `json:"groups,omitempty"`
	Sessions     []PanelSpeedSession `json:"sessions,omitempty"`
	NoTTFT       bool                `json:"noTtft,omitempty"`
	Tiered       bool                `json:"tiered,omitempty"`
	From         time.Time           `json:"from,omitempty"`
	To           time.Time           `json:"to,omitempty"`
	CalculatedAt time.Time           `json:"calculatedAt,omitempty"`
}

// PanelSpeedStat is one model/effort row; durations are nanoseconds.
type PanelSpeedStat struct {
	Model        string `json:"model"`
	Effort       string `json:"effort,omitempty"`
	Tier         string `json:"tier,omitempty"`
	Calls        int    `json:"calls,omitempty"`
	TTFTP50NS    int64  `json:"ttftP50Ns,omitempty"`
	TTFTP90NS    int64  `json:"ttftP90Ns,omitempty"`
	OutputTokens int64  `json:"outputTokens,omitempty"`
	DecodeNS     int64  `json:"decodeNs,omitempty"`
}

// PanelSpeedSession is one analysed conversation of the speed report.
type PanelSpeedSession struct {
	ID          string           `json:"id"`
	Title       string           `json:"title,omitempty"`
	ProjectName string           `json:"projectName,omitempty"`
	UpdatedAt   time.Time        `json:"updatedAt,omitempty"`
	Groups      []PanelSpeedStat `json:"groups,omitempty"`
}

// PanelAPIView mirrors the Claude API relay account view; it never holds
// the key.
type PanelAPIView struct {
	BaseURL   string          `json:"baseUrl,omitempty"`
	Quota     *apiquota.Quota `json:"quota,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
}

// PanelGrokBilling mirrors Grok's billing details for the popover.
type PanelGrokBilling struct {
	Plan                 *string             `json:"plan,omitempty"`
	IncludedUsagePercent float64             `json:"includedUsagePercent"`
	Period               *PanelBillingPeriod `json:"period,omitempty"`
	IncludedLimitCents   *int64              `json:"includedLimitCents,omitempty"`
	IncludedUsedCents    *int64              `json:"includedUsedCents,omitempty"`
	PrepaidBalanceCents  *int64              `json:"prepaidBalanceCents,omitempty"`
	OnDemandEnabled      *bool               `json:"onDemandEnabled,omitempty"`
	OnDemandUsedCents    *int64              `json:"onDemandUsedCents,omitempty"`
	OnDemandLimitCents   *int64              `json:"onDemandLimitCents,omitempty"`
	ProductUsage         []PanelProductUsage `json:"productUsage,omitempty"`
	IsUnifiedBillingUser *bool               `json:"isUnifiedBillingUser,omitempty"`
	ResetCredits         *PanelResetCredits  `json:"resetCredits,omitempty"`
	Source               string              `json:"source,omitempty"`
	UpdatedAt            time.Time           `json:"updatedAt"`
}

// PanelBillingPeriod is the included-credits window.
type PanelBillingPeriod struct {
	Kind     string     `json:"kind"` // weekly | monthly
	StartsAt *time.Time `json:"startsAt,omitempty"`
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
}

// PanelProductUsage is one product's share of the included usage.
type PanelProductUsage struct {
	Product      string  `json:"product"`
	UsagePercent float64 `json:"usagePercent"`
}
