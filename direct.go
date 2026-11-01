package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	secretsURL            = "https://code.thetadev.de/ThetaDev/spotify-secrets/raw/branch/main/secrets/secretDict.json"
	secretRefreshInterval = time.Hour
	directTimeout         = 5 * time.Second
)

type totpSecret struct {
	version int
	secret  []int
}

func (s *server) getDirectAccessTokenPayload(ctx context.Context, cookies []*http.Cookie) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, directTimeout)
	defer cancel()

	secret := s.secret.Load()
	if secret == nil {
		return nil, errors.New("no totp secret loaded")
	}
	return requestToken(ctx, secret, cookies)
}

func (s *server) secretLoop() {
	for {
		wait := secretRefreshInterval
		if s.secret.Load() == nil {
			wait = retryDelay
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(wait):
		}
		s.refreshSecret()
	}
}

func (s *server) refreshSecret() {
	ctx, cancel := context.WithTimeout(s.ctx, directTimeout)
	defer cancel()

	secret, err := latestSecret(ctx)
	if err != nil {
		slog.Error("Failed to refresh totp secret", slog.Any("err", err))
		return
	}
	s.secret.Store(secret)
	slog.Info("Refreshed totp secret", slog.Int("version", secret.version))
}

func requestToken(ctx context.Context, secret *totpSecret, cookies []*http.Cookie) ([]byte, error) {
	code := totp(totpKey(secret.secret), time.Now())
	query := url.Values{
		"reason":      {"init"},
		"productType": {"mobile-web-player"},
		"totp":        {code},
		"totpServer":  {code},
		"totpVer":     {strconv.Itoa(secret.version)},
	}
	body, err := get(ctx, spotifyTokenURL+"?"+query.Encode(), cookies)
	if err != nil {
		return nil, err
	}
	if _, err = tokenExpiry(body); err != nil {
		return nil, err
	}
	return body, nil
}

func latestSecret(ctx context.Context) (*totpSecret, error) {
	body, err := get(ctx, secretsURL, nil)
	if err != nil {
		return nil, err
	}

	var secrets map[string][]int
	if err = json.Unmarshal(body, &secrets); err != nil {
		return nil, fmt.Errorf("failed to parse secrets: %w", err)
	}

	var latest *totpSecret
	for k, v := range secrets {
		if n, err := strconv.Atoi(k); err == nil && len(v) > 0 && (latest == nil || n > latest.version) {
			latest = &totpSecret{version: n, secret: v}
		}
	}
	if latest == nil {
		return nil, errors.New("no secrets found")
	}
	return latest, nil
}

func get(ctx context.Context, url string, cookies []*http.Cookie) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	rs, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer rs.Body.Close()

	body, err := io.ReadAll(io.LimitReader(rs.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if rs.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", rs.StatusCode, req.URL.Host)
	}
	return body, nil
}

func totpKey(secret []int) []byte {
	var b strings.Builder
	for i, c := range secret {
		b.WriteString(strconv.Itoa(c ^ (i%33 + 9)))
	}
	return []byte(b.String())
}

func totp(key []byte, t time.Time) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(t.Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0xf
	code := binary.BigEndian.Uint32(sum[offset:]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1_000_000)
}
