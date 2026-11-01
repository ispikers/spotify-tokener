package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const (
	refreshBefore = 5 * time.Minute
	retryDelay    = 10 * time.Second
	fetchTimeout  = 30 * time.Second
)

func (s *server) handleToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var (
		body []byte
		err  error
	)
	if len(r.Cookies()) == 0 {
		body, err = s.cachedToken(ctx)
	} else {
		body, err = s.accessTokenPayload(ctx, r.Cookies())
	}
	if err != nil {
		if ctx.Err() != nil {
			slog.WarnContext(ctx, "Client gave up waiting for access token payload", slog.Any("err", err))
		} else {
			slog.ErrorContext(ctx, "Failed to get access token payload", slog.Any("err", err))
		}
		http.Error(w, "Failed to get access token payload", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err = w.Write(body); err != nil {
		slog.ErrorContext(ctx, "Failed to write response", slog.Any("err", err))
	}
}

func (s *server) accessTokenPayload(ctx context.Context, cookies []*http.Cookie) ([]byte, error) {
	body, err := s.getDirectAccessTokenPayload(ctx, cookies)
	if err == nil {
		return body, nil
	}
	slog.WarnContext(ctx, "Direct access token fetch failed, falling back to browser", slog.Any("err", err))

	params := make([]*network.CookieParam, 0, len(cookies))
	for _, cookie := range cookies {
		params = append(params, &network.CookieParam{
			Name:  cookie.Name,
			Value: cookie.Value,
			URL:   spotifyURL,
		})
	}
	return s.getAccessTokenPayload(ctx, params)
}

func (s *server) cachedToken(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	if time.Now().Before(s.expires) {
		defer s.mu.Unlock()
		return s.token, nil
	}
	done := s.startRefreshLocked()
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Now().Before(s.expires) {
		return s.token, nil
	}
	if s.refreshErr != nil {
		return nil, s.refreshErr
	}
	return nil, errors.New("access token expired")
}

func (s *server) refreshLoop() {
	for {
		s.mu.Lock()
		done := s.startRefreshLocked()
		s.mu.Unlock()
		<-done

		s.mu.Lock()
		wait := max(time.Until(s.expires)-refreshBefore, retryDelay)
		s.mu.Unlock()

		select {
		case <-s.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (s *server) startRefreshLocked() <-chan struct{} {
	if s.refreshing == nil {
		s.refreshing = make(chan struct{})
		go s.refresh(s.refreshing)
	}
	return s.refreshing
}

func (s *server) refresh(done chan struct{}) {
	ctx, cancel := context.WithTimeout(s.ctx, fetchTimeout)
	defer cancel()

	start := time.Now()
	body, expires, err := s.fetchToken(ctx)
	if err != nil {
		slog.Error("Failed to refresh anonymous access token", slog.Duration("took", time.Since(start)), slog.Any("err", err))
	} else {
		slog.Info("Refreshed anonymous access token", slog.Duration("took", time.Since(start)), slog.Time("expires", expires))
	}

	s.mu.Lock()
	s.refreshing = nil
	s.refreshErr = err
	if err == nil {
		s.token, s.expires = body, expires
	}
	s.mu.Unlock()
	close(done)
}

func (s *server) fetchToken(ctx context.Context) ([]byte, time.Time, error) {
	body, err := s.fetch(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}

	expires, err := tokenExpiry(body)
	if err != nil {
		return nil, time.Time{}, err
	}
	return body, expires, nil
}

func tokenExpiry(body []byte) (time.Time, error) {
	var payload struct {
		AccessTokenExpirationTimestampMs int64 `json:"accessTokenExpirationTimestampMs"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return time.Time{}, fmt.Errorf("failed to parse access token payload: %w", err)
	}

	expires := time.UnixMilli(payload.AccessTokenExpirationTimestampMs)
	if !expires.After(time.Now()) {
		return time.Time{}, errors.New("access token payload has no future expiration")
	}
	return expires, nil
}

func (s *server) getAccessTokenPayload(rCtx context.Context, cookies []*network.CookieParam) ([]byte, error) {
	slog.DebugContext(rCtx, "Getting access token payload", slog.Int("cookieCount", len(cookies)))
	ctx, cancel := chromedp.NewContext(s.ctx)
	defer cancel()

	defer context.AfterFunc(rCtx, cancel)()

	var tokenRequestID network.RequestID
	requestIDChan := make(chan network.RequestID, 1)

	chromedp.ListenTarget(ctx, func(ev any) {
		switch ev := ev.(type) {
		case *network.EventResponseReceived:
			if strings.HasPrefix(ev.Response.URL, spotifyTokenURL) {
				tokenRequestID = ev.RequestID
			}
		case *network.EventLoadingFinished:
			if ev.RequestID != tokenRequestID {
				return
			}
			select {
			case requestIDChan <- ev.RequestID:
			default:
			}
		}
	})

	if err := chromedp.Run(ctx,
		network.SetCookies(cookies),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, errorText, err := page.Navigate(spotifyURL).Do(ctx)
			if err != nil {
				return err
			}
			if errorText != "" {
				return fmt.Errorf("failed to navigate: %s", errorText)
			}
			return nil
		}),
	); err != nil {
		return nil, err
	}

	var requestID network.RequestID
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case requestID = <-requestIDChan:
	}

	var body []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		body, err = network.GetResponseBody(requestID).Do(ctx)
		return err
	})); err != nil {
		return nil, err
	}

	return body, nil
}
