package main

import (
	"context"
	"crypto/tls"
	"embed"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/firefart/zwiebelproxy/antikorpsLogger"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

type application struct {
	transport *http.Transport
	domain    string
	timeout   time.Duration
	logger    Logger
	templates *template.Template

	JsonLogger        antikorpsLogger.MyJsonLogger
	JsonLoggerEnabled bool
}

var (
	//go:embed templates
	templateFS embed.FS
)

func main() {
	log := logrus.New()
	log.SetOutput(os.Stdout)
	log.SetLevel(logrus.InfoLevel)

	err := godotenv.Load()
	if err != nil {
		log.Warnf("could not load .env file: %v. continuing without", err)
	}

	host := flag.String("host", lookupEnvOrString(log, "ZWIEBEL_HOST", "127.0.0.1:8080"), "IP and Port to bind to. You can also use the ZWIEBEL_HOST environment variable or an entry in the .env file to set this parameter.")
	debug := flag.Bool("debug", lookupEnvOrBool(log, "ZWIEBEL_DEBUG", false), "Enable DEBUG mode. You can also use the ZWIEBEL_DEBUG environment variable or an entry in the .env file to set this parameter.")
	domain := flag.String("domain", lookupEnvOrString(log, "ZWIEBEL_DOMAIN", ""), "domain to use. You can also use the ZWIEBEL_DOMAIN environment variable or an entry in the .env file to set this parameter.")
	tor := flag.String("tor", lookupEnvOrString(log, "ZWIEBEL_TOR", "socks5://127.0.0.1:9050"), "TOR Proxy server. You can also use the ZWIEBEL_TOR environment variable or an entry in the .env file to set this parameter.")
	wait := flag.Duration("graceful-timeout", lookupEnvOrDuration(log, "ZWIEBEL_GRACEFUL_TIMEOUT", 5*time.Second), "the duration for which the server gracefully wait for existing connections to finish - e.g. 15s or 1m. You can also use the ZWIEBEL_GRACEFUL_TIMEOUT environment variable or an entry in the .env file to set this parameter.")
	timeout := flag.Duration("timeout", lookupEnvOrDuration(log, "ZWIEBEL_TIMEOUT", 5*time.Minute), "http timeout. You can also use the ZWIEBEL_TIMEOUT environment variable or an entry in the .env file to set this parameter.")
	jsonPath := flag.String("jsonpath", "", "absolute path folder for the json log files")
	var jsonLoggerEnabled bool
	var jsonLogger antikorpsLogger.MyJsonLogger

	flag.Parse()

	if *jsonPath != "" {
		jsonLoggerEnabled = true
		jsonLogger = antikorpsLogger.NewJsonLogger(*jsonPath)
	}
	jsonPathEnv := os.Getenv("jsonpath")
	if !jsonLoggerEnabled && jsonPathEnv != "" {
		jsonLoggerEnabled = true
		jsonLogger = antikorpsLogger.NewJsonLogger(jsonPathEnv)
	}

	if *debug {
		log.SetLevel(logrus.DebugLevel)
		log.Debug("DEBUG mode enabled")
		if jsonLoggerEnabled {
			jsonLogger.DebugLevel("DEBUG mode enabled")
		}
	}

	if len(*domain) == 0 {
		log.Errorf("please provide a domain")
		if jsonLoggerEnabled {
			jsonLogger.ErrorLevel("please provide a domain")
		}
		os.Exit(1)
	}

	if !strings.HasPrefix(*domain, ".") {
		var a = fmt.Sprintf(".%s", *domain)
		domain = &a
	}

	torProxyURL, err := url.Parse(*tor)
	if err != nil {
		log.Errorf("invalid proxy url %s: %v", *tor, err)
		if jsonLoggerEnabled {
			message := fmt.Sprintf("invalid proxy url %s: %v", *tor, err)
			jsonLogger.ErrorLevel(message)
		}
		os.Exit(1)
	}

	// used to clone the default transport
	tr := http.DefaultTransport.(*http.Transport)
	tr.Proxy = http.ProxyURL(torProxyURL)
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	tr.TLSHandshakeTimeout = *timeout
	tr.ExpectContinueTimeout = *timeout
	tr.ResponseHeaderTimeout = *timeout

	tr.DialContext = (&net.Dialer{
		Timeout:   *timeout,
		KeepAlive: *timeout,
	}).DialContext

	app := &application{
		transport:         tr,
		domain:            *domain,
		timeout:           *timeout,
		logger:            log,
		templates:         template.Must(template.ParseFS(templateFS, "templates/*.tmpl")),
		JsonLogger:        jsonLogger,
		JsonLoggerEnabled: jsonLoggerEnabled,
	}

	srv := &http.Server{
		Addr:    *host,
		Handler: app.routes(),
	}
	log.Infof("Starting server on %s", *host)
	if jsonLoggerEnabled {
		message := fmt.Sprintf("Starting server on %s", *host)
		jsonLogger.DebugLevel(message)
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Error(err)
			if jsonLoggerEnabled {
				jsonLogger.ErrorLevel(err.Error())
			}
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	<-c
	ctx, cancel := context.WithTimeout(context.Background(), *wait)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error(err)
		if jsonLoggerEnabled {
			jsonLogger.ErrorLevel(err.Error())
		}
	}
	log.Info("shutting down")
	if jsonLoggerEnabled {
		jsonLogger.DebugLevel("shutting down")
	}
	os.Exit(0)
}

func (app *application) routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(app.xHeaderMiddleware)
	r.Use(middleware.Recoverer)

	ph := http.HandlerFunc(app.proxyHandler)
	r.Handle("/*", ph)
	return r
}

func (app *application) logError(w http.ResponseWriter, err error, statusCode int) {
	w.WriteHeader(statusCode)
	w.Header().Set("Connection", "close")
	errorText := fmt.Sprintf("%v", err)
	app.logger.Error(errorText)
	if app.JsonLoggerEnabled {
		app.JsonLogger.DebugLevel(err.Error())
	}

	data := struct {
		Error string
	}{
		Error: errorText,
	}
	if err2 := app.templates.ExecuteTemplate(w, "default.tmpl", data); err2 != nil {
		app.logger.Error(err2)
		if app.JsonLoggerEnabled {
			app.JsonLogger.ErrorLevel(err2.Error())
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		if app.JsonLoggerEnabled {
			message := fmt.Sprintf("Internal Server Error %d", http.StatusInternalServerError)
			app.JsonLogger.ErrorLevel(message)
		}
	}
}

func (app *application) proxyHandler(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		// no port present
		host = r.Host
	}

	if host == strings.TrimLeft(app.domain, ".") {
		if err := app.templates.ExecuteTemplate(w, "default.tmpl", nil); err != nil {
			if app.JsonLoggerEnabled {
				message := "error on executing template:" + err.Error()
				app.JsonLogger.ErrorLevel(message)
			}
			panic(fmt.Sprintf("error on executing template: %v", err))
		}
		return
	}

	if !strings.HasSuffix(host, app.domain) {
		if app.JsonLoggerEnabled {
			message := fmt.Sprintf("invalid domain %s called. The domain needs to end in %s %d", host, app.domain, http.StatusBadRequest)
			app.JsonLogger.ErrorLevel(message)
		}
		app.logError(w, fmt.Errorf("invalid domain %s called. The domain needs to end in %s", host, app.domain), http.StatusBadRequest)
		return
	}

	proxy := httputil.ReverseProxy{
		Director: app.director,
	}

	proxy.FlushInterval = -1
	proxy.ModifyResponse = app.modifyResponse
	proxy.Transport = app.transport
	proxy.ErrorHandler = app.proxyErrorHandler

	app.logger.Debugf("sending request %+v", r)
	if app.JsonLoggerEnabled {
		message := app.JsonLogger.LogRequestJson(r, "DEBUG", "sending request")
		app.JsonLogger.WriteToFile(message)
	}

	// set a custom timeout
	ctx, cancel := context.WithTimeout(r.Context(), app.timeout)
	defer cancel()
	r = r.WithContext(ctx)
	proxy.ServeHTTP(w, r)
}

func (app *application) xHeaderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		for headerName, headerValue := range r.Header {
			switch strings.ToLower(headerName) {
			case "x-real-ip":
				// this is already handled by the RealIP middleware
				delete(r.Header, headerName)
			case "x-forwarded-port":
				port := headerValue[0]
				host, _, err := net.SplitHostPort(r.URL.Host)
				if err != nil {
					// err occurs if no port present so append one
					if port != "" && port != "80" && port != "443" {
						r.URL.Host = net.JoinHostPort(r.URL.Host, port)
					}
				} else {
					if port != "" && port != "80" && port != "443" {
						r.URL.Host = net.JoinHostPort(host, port)
					} else {
						r.URL.Host = host
					}
				}
				host, _, err = net.SplitHostPort(r.Host)
				if err != nil {
					// err occurs if no port present so append one
					if port != "" && port != "80" && port != "443" {
						r.Host = net.JoinHostPort(r.Host, port)
					}
				} else {
					if port != "" && port != "80" && port != "443" {
						r.Host = net.JoinHostPort(host, port)
					} else {
						r.Host = host
					}
				}
				delete(r.Header, headerName)
			case "x-forwarded-proto":
				r.URL.Scheme = headerValue[0]
				delete(r.Header, headerName)
			}
		}
		next.ServeHTTP(rw, r)
	})
}
