package digest

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
)

// DigestEmailData is the template context for rendering a digest email.
type DigestEmailData struct {
	UserEmail        string
	Period           string // "Daily" or "Weekly"
	PeriodLabel      string // "yesterday" or "this week"
	DateRange        string
	GlobalHighlights []GlobalHighlight
	WatchlistEvents  []WatchlistEvent
	NotableEvents    []WatchlistEvent // only watchlist events that are notable
	QuietEvents      []WatchlistEvent // quiet watchlist events (shown as one-liners)
	Stats            FunStats
	ShowWatchlist    bool
	ShowGlobal       bool
	DashboardURL     string
	UnsubscribeURL   string
	Year             int
}

// RenderDigestEmail renders the HTML email body from digest data + user preferences.
func RenderDigestEmail(data *DigestData, user *models.User, dashboardURL, unsubToken string) (subject, htmlBody string, err error) {
	// Build template data
	td := DigestEmailData{
		UserEmail:        user.Email,
		Period:           strings.Title(data.Period),
		GlobalHighlights: data.GlobalHighlights,
		WatchlistEvents:  data.WatchlistEvents,
		Stats:            data.Stats,
		DashboardURL:     dashboardURL,
		UnsubscribeURL:   fmt.Sprintf("%s/api/digest/unsubscribe?token=%s", dashboardURL, unsubToken),
		Year:             time.Now().Year(),
	}

	if data.Period == "daily" {
		td.PeriodLabel = "yesterday"
		td.DateRange = data.PeriodStart.Format("Jan 2, 2006")
	} else {
		td.PeriodLabel = "this week"
		td.DateRange = fmt.Sprintf("%s – %s", data.PeriodStart.Format("Jan 2"), data.PeriodEnd.Format("Jan 2, 2006"))
	}

	// Split watchlist into notable vs quiet
	for _, ev := range data.WatchlistEvents {
		if ev.IsNotable {
			td.NotableEvents = append(td.NotableEvents, ev)
		} else {
			td.QuietEvents = append(td.QuietEvents, ev)
		}
	}

	td.ShowWatchlist = user.DigestContent == models.DigestContentWatchlist || user.DigestContent == models.DigestContentAll
	td.ShowGlobal = user.DigestContent == models.DigestContentGlobal || user.DigestContent == models.DigestContentAll

	// Build subject line
	subject = buildSubjectLine(data)

	// Render HTML
	tmpl, err := template.New("digest").Funcs(TemplateFuncs()).Parse(digestTemplate)
	if err != nil {
		return "", "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return "", "", fmt.Errorf("render template: %w", err)
	}

	return subject, buf.String(), nil
}

func buildSubjectLine(data *DigestData) string {
	if len(data.GlobalHighlights) > 0 {
		top := data.GlobalHighlights[0]
		return fmt.Sprintf("WikiSurge %s: \"%s\" and %d more highlights",
			strings.Title(data.Period), top.Title, len(data.GlobalHighlights)-1)
	}
	return fmt.Sprintf("Your WikiSurge %s Digest", strings.Title(data.Period))
}

// TemplateFuncs returns the template function map for digest emails.
func TemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatNumber": func(n int64) string {
			if n >= 1_000_000 {
				return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
			}
			if n >= 1_000 {
				return fmt.Sprintf("%.1fK", float64(n)/1_000)
			}
			return fmt.Sprintf("%d", n)
		},
		"formatInt": func(n int) string {
			if n >= 1_000_000 {
				return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
			}
			if n >= 1_000 {
				return fmt.Sprintf("%.1fK", float64(n)/1_000)
			}
			return fmt.Sprintf("%d", n)
		},
		"eventIcon": func(eventType string) string {
			switch eventType {
			case "spike":
				return "📈"
			case "edit_war":
				return "⚔️"
			case "trending":
				return "🔥"
			case "active":
				return "✏️"
			default:
				return "📄"
			}
		},
		"upper": strings.ToUpper,
		// intensityColor returns an accent color based on edit count magnitude.
		"intensityColor": func(edits int) string {
			switch {
			case edits >= 500:
				return "#EF4444" // red — extreme
			case edits >= 200:
				return "#F59E0B" // amber — high
			case edits >= 50:
				return "#8B5CF6" // purple — moderate
			default:
				return "#00FF88" // green — low
			}
		},
		// barWidth returns a percentage width (10-100) for a visual bar.
		"barWidth": func(pct float64) string {
			w := int(pct)
			if w < 8 {
				w = 8
			}
			if w > 100 {
				w = 100
			}
			return fmt.Sprintf("%d%%", w)
		},
		// rankLabel returns "🥇", "🥈", "🥉" or "#N".
		"rankLabel": func(rank int) string {
			switch rank {
			case 1:
				return "🥇"
			case 2:
				return "🥈"
			case 3:
				return "🥉"
			default:
				return fmt.Sprintf("#%d", rank)
			}
		},
		// battleIntensity returns a textual intensity label.
		"battleIntensity": func(edits int) string {
			switch {
			case edits >= 500:
				return "⚠️ ALL-OUT WAR"
			case edits >= 200:
				return "🔥 HEATED"
			case edits >= 50:
				return "⚡ ACTIVE"
			default:
				return "💬 SIMMERING"
			}
		},
	}
}

// The full HTML email template — Spotify Wrapped / Letterboxd style with inline CSS.
const digestTemplate = `<!DOCTYPE html>
<html lang="en" xmlns="http://www.w3.org/1999/xhtml" xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta name="color-scheme" content="dark">
<meta name="supported-color-schemes" content="dark">
<title>WikiSurge {{.Period}} Digest</title>
<!--[if mso]>
<style>body,table,td{font-family:Arial,Helvetica,sans-serif!important;}</style>
<![endif]-->
</head>
<body style="margin:0;padding:0;background-color:#0D1117;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;-webkit-font-smoothing:antialiased;">

<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#0D1117;">
<tr><td align="center" style="padding:0;">

<!-- ============================================ -->
<!-- OUTER WRAPPER — max 600px                    -->
<!-- ============================================ -->
<table role="presentation" width="600" cellspacing="0" cellpadding="0" style="max-width:600px;width:100%;">

<!-- ============================================ -->
<!-- HERO CARD                                    -->
<!-- ============================================ -->
<tr>
<td style="padding:32px 20px 0;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#161B22;border-radius:20px;overflow:hidden;border:1px solid #30363D;">
<!-- Top gradient accent bar -->
<tr><td style="height:4px;background:linear-gradient(90deg,#00FF88 0%,#8B5CF6 50%,#F59E0B 100%);font-size:0;line-height:0;">&nbsp;</td></tr>
<tr>
<td style="padding:48px 40px 20px;text-align:center;">
<p style="margin:0;font-size:13px;font-weight:600;color:#00FF88;text-transform:uppercase;letter-spacing:3px;">Your WikiSurge</p>
<h1 style="margin:8px 0 0;color:#E6EDF3;font-size:38px;font-weight:800;letter-spacing:-1px;line-height:1.1;">{{.Period}} Digest</h1>
</td>
</tr>
<tr>
<td style="padding:0 40px 40px;text-align:center;">
<table role="presentation" cellspacing="0" cellpadding="0" align="center">
<tr>
<td style="background-color:#21262D;border-radius:20px;padding:6px 16px;">
<p style="margin:0;color:#8B949E;font-size:12px;font-weight:500;letter-spacing:0.5px;">{{.DateRange}}</p>
</td>
</tr>
</table>
<p style="margin:20px 0 0;color:#8B949E;font-size:15px;line-height:1.5;">
Here's what happened on Wikipedia {{.PeriodLabel}} ⚡
</p>
</td>
</tr>
</table>
</td>
</tr>

<!-- ============================================ -->
<!-- FUN STATS — BIG NUMBER REVEAL CARDS         -->
<!-- ============================================ -->
<tr>
<td style="padding:16px 20px 0;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#161B22;border-radius:20px;overflow:hidden;border:1px solid #30363D;">
<tr><td style="padding:32px 32px 12px;text-align:center;">
<p style="margin:0;font-size:11px;font-weight:700;color:#8B949E;text-transform:uppercase;letter-spacing:3px;">📊 Fun Stats</p>
</td></tr>

<!-- Total Edits — hero stat -->
<tr>
<td style="padding:8px 32px 24px;text-align:center;">
<p style="margin:0;font-size:64px;font-weight:800;color:#00FF88;letter-spacing:-2px;line-height:1;">{{formatNumber .Stats.TotalEdits}}</p>
<p style="margin:6px 0 0;font-size:14px;color:#8B949E;text-transform:uppercase;letter-spacing:2px;">edits tracked</p>
</td>
</tr>

<!-- Divider -->
<tr><td style="padding:0 32px;"><table role="presentation" width="100%"><tr><td style="height:1px;background-color:#21262D;font-size:0;line-height:0;">&nbsp;</td></tr></table></td></tr>

<!-- Two-col: Edit Wars + Top Language -->
<tr>
<td style="padding:24px 20px 32px;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0">
<tr>
<td width="50%" style="text-align:center;padding:0 12px;vertical-align:top;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#1C1017;border-radius:16px;border:1px solid #3D1F1F;">
<tr><td style="padding:24px 16px;">
<p style="margin:0;font-size:42px;font-weight:800;color:#EF4444;line-height:1;">{{.Stats.EditWars}}</p>
<p style="margin:6px 0 0;font-size:11px;color:#8B949E;text-transform:uppercase;letter-spacing:2px;">Edit Wars</p>
<p style="margin:6px 0 0;font-size:18px;">⚔️</p>
</td></tr>
</table>
</td>
<td width="50%" style="text-align:center;padding:0 12px;vertical-align:top;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#0D1A2D;border-radius:16px;border:1px solid #1F3D5C;">
<tr><td style="padding:24px 16px;">
{{if .Stats.TopLanguages}}
<p style="margin:0;font-size:42px;font-weight:800;color:#3B82F6;line-height:1;">{{upper (index .Stats.TopLanguages 0).Language}}</p>
<p style="margin:6px 0 0;font-size:11px;color:#8B949E;text-transform:uppercase;letter-spacing:2px;">Top Language</p>
<p style="margin:6px 0 0;font-size:18px;">🌍</p>
{{else}}
<p style="margin:0;font-size:42px;font-weight:800;color:#3B82F6;line-height:1;">—</p>
<p style="margin:6px 0 0;font-size:11px;color:#8B949E;text-transform:uppercase;letter-spacing:2px;">Top Language</p>
<p style="margin:6px 0 0;font-size:18px;">🌍</p>
{{end}}
</td></tr>
</table>
</td>
</tr>
</table>
</td>
</tr>

<!-- Language Breakdown Bars -->
{{if gt (len .Stats.TopLanguages) 1}}
<tr><td style="padding:0 32px;"><table role="presentation" width="100%"><tr><td style="height:1px;background-color:#21262D;font-size:0;line-height:0;">&nbsp;</td></tr></table></td></tr>
<tr>
<td style="padding:20px 32px 28px;">
<p style="margin:0 0 14px;font-size:11px;font-weight:700;color:#8B949E;text-transform:uppercase;letter-spacing:2px;">Language Breakdown</p>
{{range .Stats.TopLanguages}}
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="margin-bottom:8px;">
<tr>
<td width="40" style="padding:0;vertical-align:middle;">
<p style="margin:0;font-size:12px;font-weight:700;color:#E6EDF3;">{{upper .Language}}</p>
</td>
<td style="padding:0 0 0 8px;vertical-align:middle;">
<table role="presentation" cellspacing="0" cellpadding="0" width="100%">
<tr>
<td style="background-color:#21262D;border-radius:6px;padding:0;">
<table role="presentation" cellspacing="0" cellpadding="0" width="{{barWidth .Percentage}}">
<tr><td style="height:10px;background:linear-gradient(90deg,#8B5CF6,#00FF88);border-radius:6px;font-size:0;line-height:0;">&nbsp;</td></tr>
</table>
</td>
</tr>
</table>
</td>
<td width="48" style="padding:0 0 0 8px;vertical-align:middle;text-align:right;">
<p style="margin:0;font-size:11px;color:#8B949E;">{{printf "%.1f" .Percentage}}%</p>
</td>
</tr>
</table>
{{end}}
</td>
</tr>
{{end}}

</table>
</td>
</tr>

{{if and .ShowGlobal .GlobalHighlights}}
<!-- ============================================ -->
<!-- GLOBAL HIGHLIGHTS — RANKED CARDS            -->
<!-- ============================================ -->
<tr>
<td style="padding:16px 20px 0;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#161B22;border-radius:20px;overflow:hidden;border:1px solid #30363D;">
<tr><td style="padding:28px 32px 8px;">
<p style="margin:0;font-size:11px;font-weight:700;color:#8B949E;text-transform:uppercase;letter-spacing:3px;">🌍 Global Highlights</p>
</td></tr>

{{range .GlobalHighlights}}
<tr>
<td style="padding:10px 24px;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#0D1117;border-radius:14px;border:1px solid #30363D;overflow:hidden;">
<!-- Intensity accent bar -->
<tr><td style="height:3px;background-color:{{intensityColor .EditCount}};font-size:0;line-height:0;">&nbsp;</td></tr>
<tr>
<td style="padding:16px 20px;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0">
<tr>
<td width="44" valign="top">
<p style="margin:0;font-size:28px;line-height:1;">{{rankLabel .Rank}}</p>
</td>
<td style="padding-left:8px;" valign="top">
<p style="margin:0;font-size:16px;font-weight:700;color:#E6EDF3;line-height:1.3;">{{eventIcon .EventType}} {{.Title}}</p>
<p style="margin:4px 0 0;font-size:13px;color:#8B949E;line-height:1.4;">{{.Summary}}</p>
</td>
<td width="70" valign="top" style="text-align:right;">
{{if gt .EditCount 0}}
<p style="margin:0;font-size:18px;font-weight:800;color:{{intensityColor .EditCount}};">{{formatInt .EditCount}}</p>
<p style="margin:2px 0 0;font-size:10px;color:#8B949E;text-transform:uppercase;letter-spacing:1px;">edits</p>
{{end}}
</td>
</tr>
</table>
</td>
</tr>
</table>
</td>
</tr>
{{end}}

<tr><td style="padding:0 0 24px;">&nbsp;</td></tr>
</table>
</td>
</tr>
{{end}}

{{if and .ShowWatchlist (or .NotableEvents .QuietEvents)}}
<!-- ============================================ -->
<!-- YOUR WATCHLIST — PERSONAL SECTION           -->
<!-- ============================================ -->
<tr>
<td style="padding:16px 20px 0;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#161B22;border-radius:20px;overflow:hidden;border:1px solid #30363D;">
<!-- Green accent bar for personal section -->
<tr><td style="height:3px;background:linear-gradient(90deg,#00FF88,#10B981);font-size:0;line-height:0;">&nbsp;</td></tr>
<tr><td style="padding:28px 32px 8px;">
<p style="margin:0;font-size:11px;font-weight:700;color:#00FF88;text-transform:uppercase;letter-spacing:3px;">📋 Your Watchlist</p>
</td></tr>

{{range .NotableEvents}}
<tr>
<td style="padding:10px 24px;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#0D1117;border-radius:14px;border-left:4px solid {{intensityColor .EditCount}};overflow:hidden;">
<tr>
<td style="padding:16px 20px;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0">
<tr>
<td valign="top">
<p style="margin:0;font-size:16px;font-weight:700;color:#E6EDF3;">{{eventIcon .EventType}} {{.Title}}</p>
<p style="margin:4px 0 0;font-size:13px;color:#8B949E;">{{.Summary}}</p>
{{if gt .EditCount 0}}
<table role="presentation" cellspacing="0" cellpadding="0" style="margin-top:8px;">
<tr>
<td style="background-color:{{intensityColor .EditCount}};border-radius:10px;padding:2px 10px;">
<p style="margin:0;font-size:11px;font-weight:700;color:#0D1117;">{{formatInt .EditCount}} edits</p>
</td>
<td style="padding-left:8px;">
<p style="margin:0;font-size:11px;font-weight:600;color:{{intensityColor .EditCount}};">{{battleIntensity .EditCount}}</p>
</td>
</tr>
</table>
{{end}}
</td>
</tr>
</table>
</td>
</tr>
</table>
</td>
</tr>
{{end}}

{{if .QuietEvents}}
<tr>
<td style="padding:10px 32px 8px;">
<p style="margin:0;font-size:11px;font-weight:600;color:#484F58;text-transform:uppercase;letter-spacing:1px;">Also on your radar</p>
</td>
</tr>
{{range .QuietEvents}}
<tr>
<td style="padding:4px 32px;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0">
<tr>
<td width="8" valign="top" style="padding-top:6px;">
<table role="presentation" cellspacing="0" cellpadding="0"><tr><td style="width:6px;height:6px;background-color:#30363D;border-radius:50%;font-size:0;line-height:0;">&nbsp;</td></tr></table>
</td>
<td style="padding-left:10px;">
<p style="margin:0;font-size:13px;color:#8B949E;"><span style="color:#C9D1D9;font-weight:600;">{{.Title}}</span> — {{.Summary}}</p>
</td>
</tr>
</table>
</td>
</tr>
{{end}}
{{end}}

<tr><td style="padding:0 0 24px;">&nbsp;</td></tr>
</table>
</td>
</tr>
{{end}}

<!-- ============================================ -->
<!-- CTA BUTTON                                   -->
<!-- ============================================ -->
<tr>
<td style="padding:24px 20px 0;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background-color:#161B22;border-radius:20px;overflow:hidden;border:1px solid #30363D;">
<tr>
<td style="padding:36px 32px;text-align:center;">
<p style="margin:0 0 20px;color:#8B949E;font-size:14px;">Want the full picture?</p>
<table role="presentation" cellspacing="0" cellpadding="0" align="center">
<tr>
<td style="border-radius:14px;background:linear-gradient(135deg,#00FF88 0%,#00D472 100%);overflow:hidden;">
<a href="{{.DashboardURL}}" style="display:inline-block;padding:16px 40px;color:#0D1117;text-decoration:none;font-size:16px;font-weight:700;letter-spacing:0.3px;">
See Live Dashboard →
</a>
</td>
</tr>
</table>
</td>
</tr>
</table>
</td>
</tr>

<!-- ============================================ -->
<!-- FOOTER                                       -->
<!-- ============================================ -->
<tr>
<td style="padding:32px 20px 40px;text-align:center;">
<table role="presentation" cellspacing="0" cellpadding="0" align="center">
<tr><td>
<p style="margin:0;font-size:13px;font-weight:600;color:#484F58;">⚡ WikiSurge</p>
<p style="margin:8px 0 0;font-size:11px;color:#30363D;line-height:1.6;">
You're receiving this because you signed up for digest emails.
</p>
<p style="margin:12px 0 0;font-size:11px;">
<a href="{{.DashboardURL}}/settings" style="color:#8B5CF6;text-decoration:none;font-weight:500;">Manage preferences</a>
<span style="color:#21262D;"> &middot; </span>
<a href="{{.UnsubscribeURL}}" style="color:#484F58;text-decoration:none;">Unsubscribe</a>
</p>
<p style="margin:16px 0 0;font-size:10px;color:#21262D;">
© {{.Year}} WikiSurge
</p>
</td></tr>
</table>
</td>
</tr>

</table>
<!-- End outer wrapper -->

</td></tr>
</table>
</body>
</html>`
