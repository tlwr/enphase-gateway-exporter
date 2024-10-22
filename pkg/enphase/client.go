package enphase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	loginURL = "https://enlighten.enphaseenergy.com/login/login.json"
	tokenURL = "http://entrez.enphaseenergy.com/tokens"
)

type LoginPageResponse struct {
	SessionID string `json:"session_id"`
}

type WebTokenRequest struct {
	SessionID string `json:"session_id"`
	Serial    string `json:"serial_num"`
	Username  string `json:"username"`
}

type EnphaseTokenManager struct {
	username string
	password string
	serial   string

	httpc *http.Client

	tokenMu sync.Mutex
	token   string

	stopWaitingForToken chan (struct{})
}

func NewManager(username, password, serial string) *EnphaseTokenManager {
	return &EnphaseTokenManager{
		username: username,
		password: password,
		serial:   serial,

		stopWaitingForToken: make(chan struct{}),

		httpc: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Dial: (&net.Dialer{
					Timeout:   15 * time.Second,
					KeepAlive: 15 * time.Second,
				}).Dial,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
			},
		},
	}
}

func (m *EnphaseTokenManager) Start(ctx context.Context) error {
	runImmediately := time.After(0)
	period := time.NewTicker(7 * 24 * time.Hour)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-runImmediately:
			err := m.Do(ctx)
			if err != nil {
				return err
			}
			m.stopWaitingForToken <- struct{}{}
		case <-period.C:
			err := m.Do(ctx)
			if err != nil {
				log.Printf("kon geen nieuwe token krijgen: %s", err)
			}
		}
	}
}

func (m *EnphaseTokenManager) FetchSessionID(ctx context.Context) (string, error) {
	log.Println("session ID aan het halen")
	defer log.Println("session ID gehaald")
	form := url.Values{}

	form.Set("user[email]", m.username)
	form.Set("user[password]", m.password)
	body := bytes.NewBufferString(form.Encode())

	req, err := http.NewRequestWithContext(ctx, "POST", loginURL, body)
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpc.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf(
			"onverwachte status %d voor %s: %s",
			resp.StatusCode, loginURL, string(b),
		)
	}

	var p LoginPageResponse
	if err := json.Unmarshal(b, &p); err != nil {
		return "", err
	}

	return p.SessionID, nil
}

func (m *EnphaseTokenManager) FetchToken(ctx context.Context, sessionID string) (string, error) {
	log.Println("token aan het halen")
	defer log.Println("token gehaald")

	payload := &WebTokenRequest{
		SessionID: sessionID,

		Username: m.username,
		Serial:   m.serial,
	}

	b, err := json.Marshal(&payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewReader(b))
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := m.httpc.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf(
			"onverwachte status %d voor %s: %s",
			resp.StatusCode, tokenURL, string(b),
		)
	}

	return string(rb), nil
}

func (m *EnphaseTokenManager) Do(ctx context.Context) error {
	sessionID, err := m.FetchSessionID(ctx)
	if err != nil {
		return fmt.Errorf("kon geen session ID halen: %w", err)
	}

	token, err := m.FetchToken(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("kon geen token halen: %w", err)
	}

	log.Println("token aan het opslagen")
	m.tokenMu.Lock()
	m.token = token
	m.tokenMu.Unlock()
	log.Println("token opgeslagen")

	return nil
}

func (m *EnphaseTokenManager) GetToken() string {
	m.tokenMu.Lock()
	defer m.tokenMu.Unlock()
	return m.token
}

// wait forever or timeout
func (m *EnphaseTokenManager) WaitForToken(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("wachten voor de token duurde te lang")
	case <-m.stopWaitingForToken:
		return nil
	}
}
