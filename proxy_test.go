package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDirector(t *testing.T) {
	t.Parallel()

	const domain = "onion.zwiebel"
	tests := []struct {
		url            string
		expectedPort   string
		expectedScheme string
		expectedHost   string
	}{
		{fmt.Sprintf("http://asdf.%s/1234", domain), "", "http", "asdf.onion"},
		{fmt.Sprintf("https://asdf.%s/1234", domain), "", "https", "asdf.onion"},
		{fmt.Sprintf("http://asdf.%s:8008/1234", domain), "8008", "http", "asdf.onion:8008"},
		{fmt.Sprintf("https://asdf.%s:8008/1234", domain), "8008", "https", "asdf.onion:8008"},
	}
	for _, tt := range tests {
		tt := tt // NOTE: https://github.com/golang/go/wiki/CommonMistakes#using-goroutines-on-loop-iterator-variables
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel() // marks each test case as capable of running in parallel with each other

			r, err := http.NewRequest(http.MethodGet, tt.url, nil)
			if err != nil {
				t.Error(err)
				return
			}
			app := application{
				domain: domain,
				logger: &DiscardLogger{},
			}
			app.director(r)
			assert.Empty(t, r.Header.Get("X-Forwarded-For"))
			assert.Equal(t, tt.expectedHost, r.Host)
			assert.Equal(t, tt.expectedScheme, r.URL.Scheme)
			assert.Equal(t, tt.expectedHost, r.URL.Host)
			assert.Equal(t, tt.expectedPort, r.URL.Port())
		})
	}
}

func TestDirectorWebRequest(t *testing.T) {
	t.Parallel()

	const domain = "onion.zwiebel"
	tests := []struct {
		path           string
		host           string
		expectedPort   string
		expectedScheme string
		expectedHost   string
	}{
		{"/1234", fmt.Sprintf("asdf.%s", domain), "", "http", "asdf.onion"},
		{"/1234", fmt.Sprintf("asdf.%s", domain), "", "http", "asdf.onion"},
		{"/1234", fmt.Sprintf("asdf.%s:8008", domain), "8008", "http", "asdf.onion:8008"},
		{"/1234", fmt.Sprintf("asdf.%s:8008", domain), "8008", "http", "asdf.onion:8008"},
		{"/1234", fmt.Sprintf("asdf.%s:80", domain), "", "http", "asdf.onion"},
		{"/1234", fmt.Sprintf("asdf.%s:443", domain), "", "https", "asdf.onion"},
	}
	for _, tt := range tests {
		tt := tt // NOTE: https://github.com/golang/go/wiki/CommonMistakes#using-goroutines-on-loop-iterator-variables
		t.Run("", func(t *testing.T) {
			t.Parallel() // marks each test case as capable of running in parallel with each other

			r, err := http.NewRequest(http.MethodGet, "http://test.com", nil)
			if err != nil {
				t.Error(err)
				return
			}

			// an incoming webrequest looks like this
			r.URL.Scheme = ""
			r.URL.Host = ""
			r.URL.Path = tt.path
			r.URL.RawPath = ""
			r.Host = tt.host

			app := application{
				domain: domain,
				logger: &DiscardLogger{},
			}
			app.director(r)
			assert.Empty(t, r.Header.Get("X-Forwarded-For"))
			assert.Equal(t, tt.expectedHost, r.Host)
			assert.Equal(t, tt.expectedScheme, r.URL.Scheme)
			assert.Equal(t, tt.expectedHost, r.URL.Host)
			assert.Equal(t, tt.expectedPort, r.URL.Port())
		})
	}
}

func TestModifyResponse(t *testing.T) {
	t.Parallel()

	const domain = "xxx.zwiebel"
	body := []byte("asfasdf najngkjsdngsdngskjgnskjngdfg.onion safdsdfa akjfajfklf.onion/asdfasf")
	tests := []struct {
		name        string
		download    bool
		contentType string
		body        []byte
	}{
		{"empty", false, "", body},
		{"download", true, "text/plain", body},
		{"plain", false, "text/plain", body},
		{"octet-stream", false, "application/octet-stream", body},
	}
	for _, tt := range tests {
		tt := tt // NOTE: https://github.com/golang/go/wiki/CommonMistakes#using-goroutines-on-loop-iterator-variables
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := http.Response{
				StatusCode: 200,
				Request: &http.Request{
					URL: &url.URL{},
				},
				Header: make(http.Header),
			}

			if tt.download {
				resp.Header["Content-Disposition"] = []string{`attachment; filename="filename.jpg"`}
			}

			if tt.contentType != "" {
				resp.Header["Content-Type"] = []string{tt.contentType}
			}

			resp.Body = io.NopCloser(bytes.NewBuffer(tt.body))

			app := application{
				domain: domain,
				logger: &DiscardLogger{},
			}

			if err := app.modifyResponse(&resp); err != nil {
				t.Error(err)
				return
			}

			modifiedBody, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Error(err)
				return
			}

			assert.NotContains(t, modifiedBody, domain)
		})
	}
}
