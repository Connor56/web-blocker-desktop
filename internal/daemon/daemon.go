package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"annoying-web-blocker-desktop/internal/config"
	"annoying-web-blocker-desktop/internal/hosts"
	"annoying-web-blocker-desktop/internal/web"
)

const ReconcileInterval = 30 * time.Second

type Options struct {
	ConfigPath string
	TokenPath  string
	HostsPath  string
}

func Run(ctx context.Context, options Options) error {
	return run(ctx, options, ReconcileInterval, web.Address, flushDNS)
}

func run(ctx context.Context, options Options, interval time.Duration, address string, flush func() error) error {
	tokenBytes, err := os.ReadFile(options.TokenPath)
	if err != nil {
		return fmt.Errorf("read UI token: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return errors.New("UI token is empty")
	}

	manager := hosts.NewManager(options.HostsPath)
	var coordinator sync.Mutex
	var status web.Status
	var lastValid *config.Config
	pendingDNS := true
	reconcileConfig := func(c config.Config) error {
		now := time.Now()
		domains := c.ActiveDomainsAt(now)
		active := len(domains) > 0
		changed, err := manager.Reconcile(active, domains)
		status.Checked = now
		status.Active = false
		status.Error = ""
		if err != nil {
			status.Error = err.Error()
			return err
		}
		status.Active = active
		pendingDNS = pendingDNS || changed
		if pendingDNS {
			if err := flush(); err != nil {
				status.Error = fmt.Sprintf("DNS cache flush failed: %v", err)
				return errors.New(status.Error)
			}
			pendingDNS = false
		}
		return nil
	}
	reconcile := func() error {
		coordinator.Lock()
		defer coordinator.Unlock()
		c, err := config.Load(options.ConfigPath)
		if err != nil {
			status.Error = err.Error()
			status.Checked = time.Now()
			if lastValid != nil {
				enforceErr := reconcileConfig(*lastValid)
				status.Error = "Protected configuration could not be read; continuing with the last valid schedule: " + err.Error()
				if enforceErr != nil {
					status.Error += "; enforcement error: " + enforceErr.Error()
				}
			}
			return err
		}
		lastValid = &c
		return reconcileConfig(c)
	}
	if err := reconcile(); err != nil {
		log.Printf("initial hosts reconciliation failed: %v", err)
	}

	handler := web.Handler{
		Token:                    token,
		ConfigPath:               options.ConfigPath,
		ProtectedStorageRequired: true,
		Save: func(c config.Config) error {
			coordinator.Lock()
			defer coordinator.Unlock()
			if err := config.Save(options.ConfigPath, c); err != nil {
				return err
			}
			lastValid = &c
			if err := reconcileConfig(c); err != nil {
				return fmt.Errorf("saved, but could not update hosts: %w", err)
			}
			return nil
		},
		Status: func() web.Status { coordinator.Lock(); defer coordinator.Unlock(); return status },
	}.Routes()
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("configuration UI listening at %s", web.BaseURL)
		for {
			err := server.ListenAndServe()
			select {
			case serverErrors <- err:
			case <-ctx.Done():
				return
			}
			if errors.Is(err, http.ErrServerClosed) {
				return
			}
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownContext); err != nil {
				return fmt.Errorf("shut down UI server: %w", err)
			}
			return nil
		case err := <-serverErrors:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			log.Printf("configuration UI unavailable; enforcement continues: %v", err)
		case <-ticker.C:
			if err := reconcile(); err != nil {
				log.Printf("hosts reconciliation failed: %v", err)
			}
		}
	}
}
