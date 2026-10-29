package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/hugo"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

type dashboardResponse struct {
	Content struct {
		Total     int `json:"total"`
		Published int `json:"published"`
		Drafts    int `json:"drafts"`
		Scheduled int `json:"scheduled"`
		Errors    int `json:"errors"`
		Words     int `json:"words"`
	} `json:"content"`
	Sections []sectionStat      `json:"sections"`
	Activity []monthStat        `json:"activity"`
	Recent   []hugo.ContentItem `json:"recent"`
	Themes   struct {
		Total  int    `json:"total"`
		Active string `json:"active"`
	} `json:"themes"`
	Site struct {
		Title       string `json:"title"`
		BaseURL     string `json:"base_url"`
		Theme       string `json:"theme"`
		ConfigPath  string `json:"config_path"`
		Initialized bool   `json:"initialized"`
	} `json:"site"`
	Hugo struct {
		Available     bool   `json:"available"`
		Version       string `json:"version"`
		LatestVersion string `json:"latest_version"`
		Outdated      bool   `json:"outdated"`
		Dir           string `json:"dir"`
	} `json:"hugo"`
	Build *hugo.BuildResult `json:"build"`
	Media hugo.MediaStats   `json:"media"`
}

type sectionStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type monthStat struct {
	Month string `json:"month"`
	Count int    `json:"count"`
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request, sess store.Session) {
	ctx := r.Context()
	site := s.site(ctx)
	resp := dashboardResponse{
		Sections: []sectionStat{},
		Activity: []monthStat{},
		Recent:   []hugo.ContentItem{},
	}
	items, err := site.ScanContent()
	if err != nil {
		s.logger.Error("scan content for dashboard", "error", err)
	}
	sectionCount := map[string]int{}
	activity := map[string]int{}
	now := time.Now().UTC()
	months := make([]string, 0, 6)
	for i := 5; i >= 0; i-- {
		t := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -i, 0)
		m := t.Format("2006-01")
		months = append(months, m)
		activity[m] = 0
	}
	for _, item := range items {
		resp.Content.Total++
		resp.Content.Words += item.WordCount
		switch item.Status {
		case "draft":
			resp.Content.Drafts++
		case "scheduled":
			resp.Content.Scheduled++
		case "error":
			resp.Content.Errors++
		default:
			resp.Content.Published++
		}
		name := item.Section
		if name == "" {
			name = "root"
		}
		sectionCount[name]++
		t, ok := hugo.TimeValue(item.Date)
		if !ok {
			t, _ = hugo.TimeValue(item.ModTime)
		}
		if ok {
			m := t.UTC().Format("2006-01")
			if _, exists := activity[m]; exists {
				activity[m]++
			}
		}
	}
	for name, count := range sectionCount {
		resp.Sections = append(resp.Sections, sectionStat{Name: name, Count: count})
	}
	sort.Slice(resp.Sections, func(i, j int) bool {
		if resp.Sections[i].Count != resp.Sections[j].Count {
			return resp.Sections[i].Count > resp.Sections[j].Count
		}
		return resp.Sections[i].Name < resp.Sections[j].Name
	})
	for _, m := range months {
		resp.Activity = append(resp.Activity, monthStat{Month: m, Count: activity[m]})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ModTime > items[j].ModTime })
	for _, item := range items {
		if len(resp.Recent) >= 6 {
			break
		}
		resp.Recent = append(resp.Recent, item)
	}

	if cfg, err := site.LoadConfig(); err == nil {
		resp.Site.Title = cfg.SiteTitle
		resp.Site.BaseURL = cfg.BaseURL
		resp.Site.Theme = cfg.Theme
		resp.Site.ConfigPath = cfg.ConfigPath
		resp.Site.Initialized = cfg.Initialized
	}
	if themes, err := site.ListThemes(); err == nil {
		resp.Themes.Total = len(themes)
		for _, t := range themes {
			if t.Active {
				resp.Themes.Active = t.Name
				break
			}
		}
	}
	resp.Hugo.Dir = site.Dir
	resp.Hugo.Version, resp.Hugo.Available = s.hugo.HugoVersion(ctx, site)
	resp.Hugo.LatestVersion = s.hugo.LatestHugoVersion(ctx)
	resp.Hugo.Outdated = resp.Hugo.Available && resp.Hugo.LatestVersion != "" && resp.Hugo.Version != "" &&
		!hugo.HugoVersionAtLeast(resp.Hugo.Version, resp.Hugo.LatestVersion)
	resp.Build = s.hugo.LastBuild()
	if stats, err := site.MediaStats(); err == nil {
		resp.Media = stats
	}
	writeJSON(w, http.StatusOK, resp)
}
