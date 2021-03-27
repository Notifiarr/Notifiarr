package services

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Service) validate() error {
	if s.Name == "" {
		return fmt.Errorf("%s: %w", s.Value, ErrNoName)
	} else if s.Value == "" {
		return fmt.Errorf("%s: %w", s.Name, ErrNoCheck)
	}

	switch s.Type {
	case CheckHTTP:
		if s.Expect == "" {
			s.Expect = "200"
		}
	case CheckTCP:
		if !strings.Contains(s.Value, ":") {
			return ErrBadTCP
		}
	case CheckPING:
	default:
		return ErrInvalidType
	}

	if s.Timeout.Duration == 0 {
		s.Timeout.Duration = DefaultTimeout
	} else if s.Timeout.Duration < MinimumTimeout {
		s.Timeout.Duration = MinimumTimeout
	}

	return nil
}

// startCheckers runs Parallel checkers.
func (c *Config) startCheckers() {
	for i := uint(0); i < c.Parallel; i++ {
		go func() {
			for check := range c.checks {
				check.check()
				c.done <- struct{}{}
			}

			c.done <- struct{}{}
		}()
	}
}

func (s *Service) check() {
	// check this service.
	switch s.Type {
	case CheckHTTP:
		s.checkHTTP()
	case CheckTCP:
		s.checkTCP()
	case CheckPING:
		s.checkPING()
	}
}

const maxBody = 150

func (s *Service) checkHTTP() {
	s.state = StateUnknown
	s.output = "not working yet"
	s.lastCheck = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), s.Timeout.Duration)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Value, nil)
	if err != nil {
		s.output = "creating request: " + err.Error()
		return
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		s.output = "making request: " + err.Error()
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		s.output = "reading body: " + err.Error()
		return
	}

	if strconv.Itoa(resp.StatusCode) == s.Expect {
		s.state = StateOK
		s.output = resp.Status
		return
	}

	if len(body) > maxBody {
		body = body[:maxBody]
	}

	s.state = StateCritical
	s.output = resp.Status + ": " + strings.TrimSpace(string(body))
}

func (s *Service) checkTCP() {
	s.lastCheck = time.Now()

	switch conn, err := net.DialTimeout("tcp", s.Value, s.Timeout.Duration); {
	case err != nil:
		s.state = StateCritical
		s.output = "connection error: " + err.Error()
	case conn == nil:
		s.state = StateUnknown
		s.output = "connection failed, no specific error"
	default:
		defer conn.Close()
		s.state = StateOK
		s.output = "connected to port " + strings.Split(s.Value, ":")[1] + " OK"
	}
}

func (s *Service) checkPING() {
	s.state = StateUnknown
	s.output = "ping does not work yet"
	s.lastCheck = time.Now()
}
