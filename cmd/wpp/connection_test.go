package main

import (
	"bytes"
	"context"
	"errors"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"wpp/internal/archive"
	"wpp/internal/whatsapp"
)

func TestLoginAndDiscoveryProtocolOutcomes(t *testing.T) {
	for _, scenario := range []string{"linked", "qr-error", "connect-error", "qr-closed", "timeout", "error", "success", "discover-warning", "cancel", "fatal", "post-connect-fatal", "post-connect-cancel", "chats", "chats-error", "chats-connect-error", "chats-post-connect-cancel", "chats-post-connect-fatal", "unlinked", "open-error"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				account := types.NewJID("551100000000", types.DefaultUserServer)
				linked := scenario == "linked" || strings.HasPrefix(scenario, "chats")
				events := &whatsapp.Client{Online: make(chan struct{}, 1), Fatal: make(chan error, 1)}
				qr := make(chan whatsmeow.QRChannelItem, 2)
				closed, reconnected, discovered := false, false, false
				connection := &cliConnection{events: events, account: func() *types.JID {
					if linked {
						return &account
					}
					return nil
				}, qr: func(context.Context) (<-chan whatsmeow.QRChannelItem, error) {
					if scenario == "qr-error" {
						return nil, errors.New("test QR error")
					}
					return qr, nil
				}, connect: func(context.Context) error {
					if scenario == "connect-error" || scenario == "chats-connect-error" {
						return errors.New("private connection detail")
					}
					if scenario == "cancel" {
						cancel()
					}
					if scenario == "fatal" {
						events.Fatal <- errors.New("fatal")
					}
					if scenario == "post-connect-fatal" || scenario == "chats-post-connect-fatal" {
						go func() { time.Sleep(time.Second); events.Fatal <- errors.New("fatal after connect") }()
					}
					if scenario == "post-connect-cancel" || scenario == "chats-post-connect-cancel" {
						go func() { time.Sleep(time.Second); cancel() }()
					}
					events.Online <- struct{}{}
					return nil
				}, discover: func(context.Context) error {
					discovered = true
					if scenario == "discover-warning" || scenario == "chats-error" {
						return errors.New("discovery incomplete")
					}
					return nil
				}, close: func() error { closed = true; return nil }, reconnect: func() { reconnected = true }}
				switch scenario {
				case "success", "discover-warning", "post-connect-fatal", "post-connect-cancel":
					qr <- whatsmeow.QRChannelItem{Event: "code", Code: "test QR"}
					qr <- whatsmeow.QRChannelItem{Event: "success"}
				case "timeout", "error":
					qr <- whatsmeow.QRChannelItem{Event: scenario}
				case "qr-closed":
					close(qr)
				}
				var out, stderr bytes.Buffer
				command := "login"
				if strings.HasPrefix(scenario, "chats") || scenario == "unlinked" {
					command = "chats"
				}
				err := executeWithOptions(ctx, []string{"--data-dir", t.TempDir(), command}, &out, &stderr, executeOptions{open: func(context.Context, string, *archive.Store, func(string)) (*cliConnection, error) {
					if scenario == "open-error" {
						return nil, errors.New("test open error")
					}
					return connection, nil
				}})
				wantSuccess := scenario == "linked" || scenario == "success" || scenario == "discover-warning" || scenario == "chats"
				if (err == nil) != wantSuccess {
					t.Fatal(scenario, out.String(), stderr.String(), err)
				}
				if scenario != "open-error" && !closed {
					t.Fatal("connection not closed")
				}
				if scenario == "success" || scenario == "discover-warning" {
					if !discovered || !strings.Contains(out.String(), "Conta vinculada") {
						t.Fatal(out.String())
					}
				}
				if scenario == "discover-warning" && !strings.Contains(stderr.String(), "Descoberta incompleta") {
					t.Fatal(stderr.String())
				}
				if scenario == "chats" && (!reconnected || !discovered || !strings.Contains(out.String(), "ACOMPANHANDO")) {
					t.Fatal(out.String())
				}
				if (scenario == "connect-error" || scenario == "chats-connect-error") && strings.Contains(err.Error(), "private") {
					t.Fatal("private protocol error exposed")
				}
			})
		})
	}
}

func TestDefaultCLIConnectionRequiresLoginOffline(t *testing.T) {
	var out bytes.Buffer
	err := execute(context.Background(), []string{"--data-dir", t.TempDir(), "chats"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "login primeiro") {
		t.Fatal(out.String(), err)
	}
}

func TestDefaultConnectionConfigurationWithoutNetwork(t *testing.T) {
	store, err := archive.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, store.Close()) }()
	connection, err := openCLIConnection(context.Background(), t.TempDir(), store, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { requireNoError(t, connection.close()) }()
	connection.reconnect()
	if connection.account() != nil || !connection.events.WA.InitialAutoReconnect {
		t.Fatal("default reconnect or account state incorrect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = openCLIConnection(ctx, t.TempDir(), store, func(string) {}); err == nil {
		t.Fatal("cancelled session opening succeeded")
	}
}
