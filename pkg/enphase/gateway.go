package enphase

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type Gateway struct {
	ip string

	httpc *http.Client
}

type ProductionResponse struct {
	Wh   float64 `json:"wattHoursToday"`
	Wh7d float64 `json:"wattHoursSevenDays"`
	WhL  float64 `json:"wattHoursLifetime"`
	W    float64 `json:"wattsNow"`
}

func NewGateway(ip string) *Gateway {
	return &Gateway{
		ip: ip,

		httpc: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				Dial: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 60 * time.Second,
				}).Dial,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
				// rfc 1918 adres, eg 192.168.x.y
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

func (gw *Gateway) ScrapeProduction(ctx context.Context, token string) (*ProductionResponse, error) {
	url := fmt.Sprintf("https://%s/api/v1/production", gw.ip)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := gw.httpc.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf(
			"onverwachte status %d voor %s: %s",
			resp.StatusCode, loginURL, string(b),
		)
	}

	var p ProductionResponse
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}

	return &p, nil
}
