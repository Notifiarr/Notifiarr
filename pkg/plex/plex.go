package plex

import (
	"fmt"
	"net/http"
	"time"

	"golift.io/cnfg"
)

type Server struct {
	Timeout    cnfg.Duration `toml:"timeout"`
	Interval   cnfg.Duration `toml:"interval"`
	URL        string        `toml:"url"`
	Token      string        `toml:"token"`
	AccountMap string        `toml:"account_map"`
	Name       string        `toml:"server"`
	ReturnJSON bool          `toml:"return_json"`
	Cooldown   cnfg.Duration `toml:"cooldown"`
	SeriesPC   float64       `toml:"series_percent_complete"`
	MoviesPC   float64       `toml:"movies_percent_complete"`
	client     *http.Client
}

const (
	defaultTimeout  = 10 * time.Second
	minimumTimeout  = 2 * time.Second
	defaultCooldown = 15 * time.Second
	minimumCooldown = 10 * time.Second
	minimumInterval = 5 * time.Minute
)

// WaitTime is the recommended wait time to pull plex sessions after a webhook.
const WaitTime = 10 * time.Second

var ErrNoURLToken = fmt.Errorf("token or URL for Plex missing")

// Start checks input values and starts the cron interval if it's configured.
func (s *Server) Validate() error {
	if s == nil || s.URL == "" || s.Token == "" {
		return ErrNoURLToken
	}

	if s.Interval.Duration < minimumInterval && s.Interval.Duration != 0 {
		s.Interval.Duration = minimumInterval
	}

	if s.Timeout.Duration == 0 {
		s.Timeout.Duration = defaultTimeout
	} else if s.Timeout.Duration < minimumTimeout {
		s.Timeout.Duration = minimumTimeout
	}

	if s.Cooldown.Duration == 0 {
		s.Cooldown.Duration = defaultCooldown
	} else if s.Cooldown.Duration > minimumCooldown {
		s.Cooldown.Duration = minimumCooldown
	}

	if s.Cooldown.Duration < s.Timeout.Duration {
		s.Cooldown.Duration = s.Timeout.Duration
	}

	return nil
}
