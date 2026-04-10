package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	flagServeAddr    string
	flagAllowedEmail string
)

const (
	maxMCPRequestBytes = 1 << 20
	readHeaderTimeout  = 5 * time.Second
	readTimeout        = 15 * time.Second
	writeTimeout       = 30 * time.Second
	idleTimeout        = 60 * time.Second
	maxHeaderBytes     = 1 << 20
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the MCP server over HTTP (Cloud Run friendly)",
	RunE: func(cmd *cobra.Command, args []string) error {
		addr := resolveListenAddr(flagServeAddr)
		allowed := resolveAllowedEmail(flagAllowedEmail)

		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if allowed != "" {
				if !authorizeIAPEmail(r, allowed) {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxMCPRequestBytes)
			defer r.Body.Close()
			payload, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "failed to read body", http.StatusBadRequest)
				return
			}
			payload = bytes.TrimSpace(payload)
			if len(payload) == 0 {
				http.Error(w, "empty request body", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if payload[0] == '[' {
				var reqs []jsonrpcRequest
				if err := json.Unmarshal(payload, &reqs); err != nil {
					http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
					return
				}
				var responses []jsonrpcResponse
				for i := range reqs {
					if resp, ok := processJSONRPCRequest(&reqs[i]); ok {
						responses = append(responses, *resp)
					}
				}
				if len(responses) == 0 {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if err := json.NewEncoder(w).Encode(responses); err != nil {
					log.Printf("encode batch response: %v", err)
				}
				return
			}

			var req jsonrpcRequest
			if err := json.Unmarshal(payload, &req); err != nil {
				http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
				return
			}
			resp, ok := processJSONRPCRequest(&req)
			if !ok {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				log.Printf("encode response: %v", err)
			}
		}))

		log.Printf("MCP HTTP server listening on %s (allowed email: %s)", addr, allowed)
		server := &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
			MaxHeaderBytes:    maxHeaderBytes,
		}
		return server.ListenAndServe()
	},
}

func init() {
	serveCmd.Flags().StringVar(&flagServeAddr, "listen", "", "Address to bind the HTTP server (default :$PORT or :8080)")
	serveCmd.Flags().StringVar(&flagAllowedEmail, "allowed-email", "", "Restrict access to this Google account when behind IAP")
	rootCmd.AddCommand(serveCmd)
}

func resolveListenAddr(flagValue string) string {
	if flagValue != "" {
		if strings.HasPrefix(flagValue, ":") || strings.Contains(flagValue, ":") {
			return flagValue
		}
		return ":" + flagValue
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if strings.HasPrefix(port, ":") {
		return port
	}
	return ":" + port
}

func resolveAllowedEmail(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("ALLOWED_GOOGLE_EMAIL")
}

func authorizeIAPEmail(r *http.Request, allowed string) bool {
	header := r.Header.Get("X-Goog-Authenticated-User-Email")
	if header == "" {
		return false
	}
	header = strings.TrimPrefix(header, "accounts.google.com:")
	for _, entry := range strings.Split(allowed, ",") {
		email := strings.TrimSpace(entry)
		if email == "" {
			continue
		}
		if strings.EqualFold(header, email) {
			return true
		}
	}
	return false
}
