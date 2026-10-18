package meter

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// historyVersion is the on-disk schema version.
const historyVersion = 1

// historyMaxSamples caps stored samples per pool.
const historyMaxSamples = 1500

// historyMinSpacing is the minimum spacing between recorded samples.
const historyMinSpacing = 5 * time.Minute

// historyMaxAge prunes samples older than this.
const historyMaxAge = 30 * 24 * time.Hour

// historyMinSpan is the minimum segment span for a usable rate.
const historyMinSpan = 30 * time.Minute

// historyMaxProjection limits exhaust/AtReset projections to this horizon.
const historyMaxProjection = 30 * 24 * time.Hour

// Sample is one observed reading of one pool.
type Sample struct {
	At         time.Time
	HasUsed    bool
	Used       float64
	HasPercent bool
	Percent    float64
	HasLimit   bool
	LimitValue float64
	HasReset   bool
	ResetsAt   time.Time
}

// PoolMeta carries per-pool display metadata stored alongside samples.
type PoolMeta struct {
	Title string `json:"title"`
	Unit  string `json:"unit"`
}

// Rate is the derived consumption-rate analysis of one pool.
type Rate struct {
	ID, Title, Unit string
	IsPercent       bool // PerHour/AtReset are percentage points, not amount units
	PerHour         float64
	HasRate         bool // slope of the current (post-reset) segment
	Samples         int  // samples in the segment used
	From, To        time.Time
	ExhaustsAt      time.Time
	HasExhaust      bool
	ResetsAt        time.Time
	HasReset        bool
	AtReset         float64
	HasAtReset      bool
}

type poolFile struct {
	Meta    PoolMeta `json:"meta"`
	Samples []Sample `json:"samples"`
}

type historyFile struct {
	Version   int                            `json:"version"`
	Providers map[string]map[string]poolFile `json:"providers"`
}

// History is a persistent sample store per provider and pool.
type History struct {
	path      string
	providers map[string]map[string]*poolFile
}

// LoadHistory loads samples from path; a missing or corrupt file yields an
// empty history and no error.
func LoadHistory(path string) *History {
	h := &History{
		path:      path,
		providers: make(map[string]map[string]*poolFile),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	var f historyFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != historyVersion {
		return h
	}
	for p, pools := range f.Providers {
		m := make(map[string]*poolFile, len(pools))
		for id, pf := range pools {
			pfc := pf
			m[id] = &pfc
		}
		h.providers[p] = m
	}
	return h
}

// Record appends one Sample per pool in st and persists atomically.
func (h *History) Record(provider string, st State, now time.Time) error {
	pools := h.providers[provider]
	if pools == nil {
		pools = make(map[string]*poolFile)
		h.providers[provider] = pools
	}
	recorded := false
	for _, lim := range st.Limits {
		s := Sample{
			At:         now,
			HasUsed:    !math.IsNaN(lim.Used),
			HasPercent: lim.HasPercent && !math.IsNaN(lim.Percent),
			HasLimit:   !math.IsNaN(lim.LimitValue),
			HasReset:   lim.HasReset,
			ResetsAt:   lim.ResetsAt,
		}
		// Missing numerics stay zero: encoding/json cannot marshal NaN, and
		// readers gate every value on its Has* flag anyway.
		if s.HasUsed {
			s.Used = lim.Used
		}
		if s.HasPercent {
			s.Percent = lim.Percent
		}
		if s.HasLimit {
			s.LimitValue = lim.LimitValue
		}
		if !s.HasUsed && !s.HasPercent {
			continue
		}
		pf := pools[lim.ID]
		if pf == nil {
			pf = &poolFile{Meta: PoolMeta{Title: lim.Title, Unit: lim.Unit}}
			pools[lim.ID] = pf
		}
		if n := len(pf.Samples); n > 0 && pf.Samples[n-1].At.After(now.Add(-historyMinSpacing)) {
			// Too soon: refresh metadata only.
			pf.Meta = PoolMeta{Title: lim.Title, Unit: lim.Unit}
			continue
		}
		pf.Samples = append(pf.Samples, s)
		pf.prune(now)
		recorded = true
	}
	if !recorded {
		return nil
	}
	return h.save()
}

func (p *poolFile) prune(now time.Time) {
	cutoff := now.Add(-historyMaxAge)
	i := 0
	for ; i < len(p.Samples); i++ {
		if !p.Samples[i].At.Before(cutoff) {
			break
		}
	}
	if i > 0 {
		p.Samples = p.Samples[i:]
	}
	if len(p.Samples) > historyMaxSamples {
		p.Samples = p.Samples[len(p.Samples)-historyMaxSamples:]
	}
}

func (h *History) save() error {
	f := historyFile{Version: historyVersion, Providers: make(map[string]map[string]poolFile, len(h.providers))}
	for p, pools := range h.providers {
		m := make(map[string]poolFile, len(pools))
		for id, pf := range pools {
			m[id] = poolFile{Meta: pf.Meta, Samples: pf.Samples}
		}
		f.Providers[p] = m
	}
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	dir := filepath.Dir(h.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(h.path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, h.path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Rates derives per-pool consumption-rate analysis for provider.
func (h *History) Rates(provider string, now time.Time) []Rate {
	pools := h.providers[provider]
	ids := make([]string, 0, len(pools))
	for id := range pools {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rates := make([]Rate, 0, len(ids))
	for _, id := range ids {
		pf := pools[id]
		if pf == nil || len(pf.Samples) < 2 {
			continue
		}
		rates = append(rates, analyzePool(id, pf, now))
	}
	return rates
}

// series extracts the monotone value (used, or percent when used is absent)
// from a sample and reports whether it is present.
func series(s Sample) (float64, bool) {
	if s.HasUsed {
		return s.Used, true
	}
	if s.HasPercent {
		return s.Percent, true
	}
	return 0, false
}

// analyzePool computes the Rate for one pool.
func analyzePool(id string, pf *poolFile, _ time.Time) Rate {
	samples := pf.Samples
	latest := samples[len(samples)-1]
	r := Rate{
		ID:        id,
		Title:     pf.Meta.Title,
		Unit:      pf.Meta.Unit,
		IsPercent: !latest.HasUsed && latest.HasPercent,
		Samples:   1,
		From:      latest.At,
		To:        latest.At,
		HasReset:  latest.HasReset,
		ResetsAt:  latest.ResetsAt,
	}
	if !latest.HasUsed && !latest.HasPercent {
		return r
	}

	// Walk backwards building the current segment; a real decrease cuts it.
	tolerance := 0.1
	if latest.HasLimit && !math.IsNaN(latest.LimitValue) && latest.LimitValue > 0 {
		tolerance = 0.005 * latest.LimitValue
	}
	end := len(samples) - 1
	start := end
	for i := end - 1; i >= 0; i-- {
		vNew, okNew := series(samples[i+1])
		vOld, okOld := series(samples[i])
		if !okNew || !okOld {
			break
		}
		if vNew < vOld-tolerance {
			break
		}
		start = i
	}
	seg := samples[start : end+1]
	r.Samples = len(seg)
	r.From = seg[0].At
	r.To = seg[len(seg)-1].At

	// PerHour is the true elapsed-time slope: regress the series against
	// actual sample timestamps (seconds since segment start) so variable
	// poll spacing still yields correct units-per-hour.
	var perHour float64
	span := r.To.Sub(r.From)
	if span > 0 {
		if len(seg) >= 3 {
			// Least squares over (t, value) pairs, t in seconds.
			var st, sv, stt, stv float64
			for _, s := range seg {
				v, _ := series(s)
				ts := s.At.Sub(seg[0].At).Seconds()
				st += ts
				sv += v
				stt += ts * ts
				stv += ts * v
			}
			n := float64(len(seg))
			perHour = (n*stv - st*sv) / (n*stt - st*st) * 3600
		} else {
			vNew, _ := series(seg[len(seg)-1])
			vOld, _ := series(seg[0])
			perHour = (vNew - vOld) / span.Seconds() * 3600
		}
	}
	if span >= historyMinSpan {
		r.PerHour = perHour
		r.HasRate = true
	}

	latestVal, _ := series(latest)
	if r.HasRate && perHour > 0 {
		var cap float64
		if r.IsPercent {
			cap = 100
		} else if latest.HasLimit && latest.LimitValue > 0 {
			cap = latest.LimitValue
		}
		if cap > 0 {
			hours := (cap - latestVal) / perHour
			if !math.IsInf(hours, 0) && !math.IsNaN(hours) && hours >= 0 && time.Duration(hours*float64(time.Hour)) <= historyMaxProjection {
				r.ExhaustsAt = latest.At.Add(time.Duration(hours * float64(time.Hour)))
				r.HasExhaust = true
			}
		}
	}
	if r.HasRate && r.HasReset && r.ResetsAt.After(latest.At) {
		hoursUntil := r.ResetsAt.Sub(latest.At).Hours()
		at := latestVal + perHour*hoursUntil
		clamp := 100.0
		if !r.IsPercent && latest.HasLimit && latest.LimitValue > 0 {
			clamp = latest.LimitValue
		}
		if at > clamp {
			at = clamp
		}
		if at < 0 {
			at = 0
		}
		r.AtReset = at
		r.HasAtReset = true
	}
	return r
}
