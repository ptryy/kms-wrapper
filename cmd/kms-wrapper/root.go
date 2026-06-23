// @title KMS Wrapper API
// @version 1.0
// @description REST gateway for health checks and multi-chain signing operations.
// @host localhost:8080
// @schemes http
// @BasePath /
// @securityDefinitions.bearerauth BearerAuth
// @description Bearer token authorization using Authorization: Bearer <token>.
// @security BearerAuth
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	_ "github.com/ryan-truong/kms-wrapper/docs"
	"github.com/ryan-truong/kms-wrapper/internal/config"
	"github.com/ryan-truong/kms-wrapper/internal/gateway"
	"github.com/ryan-truong/kms-wrapper/internal/keyinfo"
	cosmossigner "github.com/ryan-truong/kms-wrapper/internal/signer/cosmos"
	evmsigner "github.com/ryan-truong/kms-wrapper/internal/signer/evm"
	"github.com/ryan-truong/kms-wrapper/internal/vault"
	"github.com/ryan-truong/kms-wrapper/pkg/types"
)

type cliState struct {
	configPath string
	logLevel   string
	cfg        config.Config
}

func NewRootCommand() *cobra.Command {
	st := &cliState{}
	cmd := &cobra.Command{
		Use:   "kms-wrapper",
		Short: "kms-vault-plugin-backed multi-chain signing gateway",
	}
	home, _ := os.UserHomeDir()
	cmd.PersistentFlags().StringVar(&st.configPath, "config", filepath.Join(home, ".kms-wrapper", "config.yaml"), "config file")
	cmd.PersistentFlags().StringVar(&st.logLevel, "log-level", "info", "log level")
	cmd.AddCommand(serveCmd(st), keysCmd(st), signCmd(st), healthCmd(st))
	return cmd
}

func (s *cliState) load(warnOut io.Writer) error {
	cfg, err := config.Load(expandHome(s.configPath), func(msg string) {
		if warnOut != nil {
			_, _ = fmt.Fprintln(warnOut, msg)
		}
	})
	if err != nil {
		return err
	}
	if s.logLevel != "" {
		cfg.LogLevel = s.logLevel
	}
	s.cfg = cfg
	if err := cfg.ValidateRuntime(); err != nil {
		return err
	}
	return guardWeakVaultToken(cfg.Vault.Token, warnOut)
}

// weakVaultTokens is the set of placeholder values the gateway must refuse to
// start with outside dev mode. "root" is included because the local dev Vault
// container issues the bare root token by default — fine for `KMS_DEV=true`
// loops, not fine for production.
var weakVaultTokens = map[string]struct{}{
	"":          {},
	"root":      {},
	"dev":       {},
	"dev-token": {},
	"change-me": {},
}

// weakGatewayTokens are the placeholder values shipped in .env.example or
// commonly chosen by mistake. Empty token is rejected unconditionally via
// ValidateRuntime; this set is for additional well-known weak literals.
var weakGatewayTokens = map[string]struct{}{
	"":          {},
	"change-me": {},
	"dev":       {},
	"dev-token": {},
	"password":  {},
}

func guardWeakVaultToken(token string, warnOut io.Writer) error {
	if _, weak := weakVaultTokens[token]; !weak {
		return nil
	}
	if token == "" {
		// ValidateRuntime already covers this with the canonical message; the
		// extra weak-token guard does not need to fire.
		return nil
	}
	if os.Getenv("KMS_DEV") != "true" {
		return errors.New("refusing to start with weak vault token; set KMS_DEV=true for local dev")
	}
	if warnOut != nil {
		_, _ = fmt.Fprintln(warnOut, "warn: running with weak vault token (KMS_DEV=true)")
	}
	return nil
}

func guardWeakGatewayToken(token string, warnOut io.Writer) error {
	if _, weak := weakGatewayTokens[token]; !weak {
		return nil
	}
	if token == "" {
		return nil // ValidateRuntime already enforces non-empty
	}
	if os.Getenv("KMS_DEV") != "true" {
		return errors.New("refusing to start with weak gateway token; set KMS_DEV=true for local dev")
	}
	if warnOut != nil {
		_, _ = fmt.Fprintln(warnOut, "warn: running with weak gateway token (KMS_DEV=true)")
	}
	return nil
}

func (s *cliState) client(warnOut io.Writer) (*vault.Client, error) {
	if err := s.load(warnOut); err != nil {
		return nil, err
	}
	return vault.NewClient(s.cfg.Vault.Addr, vault.TokenAuthProvider{TokenValue: s.cfg.Vault.Token})
}

func serveCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "start REST gateway",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := st.client(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if err := guardWeakGatewayToken(st.cfg.Gateway.Token, cmd.ErrOrStderr()); err != nil {
				return err
			}
			if err := guardSwaggerNonLoopback(st.cfg, cmd.ErrOrStderr()); err != nil {
				return err
			}
			initLogger(st.cfg.LogLevel, cmd.ErrOrStderr())
			c.SetRenewalFailureHook(gateway.IncrementTokenRenewalFailure)
			c.SetVaultCallObserver(gateway.ObserveVaultCall)
			c.StartRenewal(context.Background())
			slog.Info("starting gateway", "addr", st.cfg.Gateway.Addr)
			s, err := gateway.NewOrFail(st.cfg, c, c, evmsigner.New(c), cosmossigner.New(c))
			if err != nil {
				return fmt.Errorf("init gateway: %w", err)
			}
			s.StartLimiterSweeper(context.Background())
			if st.cfg.Gateway.SwaggerEnabled {
				slog.Info("swagger UI", "url", swaggerUIURL(st.cfg))
			}
			return s.ListenAndServe()
		},
	}
}

// swaggerUIURL derives the externally-reachable Swagger UI URL using the
// same trusted-proxy/public-url precedence the resolver uses for OpenAPI
// servers[]: public_url wins, else loopback http://addr. Operators have
// asked for this so they can pull the link from the startup line directly.
func swaggerUIURL(cfg config.Config) string {
	raw := strings.TrimSpace(cfg.Gateway.PublicURL)
	if raw != "" {
		return strings.TrimRight(raw, "/") + "/swagger/index.html"
	}
	addr := cfg.Gateway.Addr
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	return "http://" + addr + "/swagger/index.html"
}

// guardSwaggerNonLoopback refuses to start when swagger is unauthenticated
// and the listen address is not a loopback IP — a public listener pointing
// at the unauthenticated swagger surface is the spec's worst-case dev/prod
// confusion. KMS_DEV=true downgrades the refusal to a warn line so the
// docker-compose loop keeps working.
func guardSwaggerNonLoopback(cfg config.Config, warnOut io.Writer) error {
	if cfg.Gateway.SwaggerAuth {
		return nil
	}
	host, _, err := net.SplitHostPort(cfg.Gateway.Addr)
	if err != nil {
		// Unparseable addr — bail with a generic message rather than
		// pretending the address is loopback.
		return fmt.Errorf("parse gateway addr %q: %w", cfg.Gateway.Addr, err)
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	if host == "localhost" {
		return nil
	}
	if os.Getenv("KMS_DEV") != "true" {
		return errors.New("refusing to expose unauthenticated swagger on non-loopback address; set KMS_DEV=true for local dev")
	}
	if warnOut != nil {
		_, _ = fmt.Fprintln(warnOut, "warn: running with unauthenticated swagger on non-loopback (KMS_DEV=true)")
	}
	return nil
}

func keysCmd(st *cliState) *cobra.Command {
	var path, prefix, chainsRaw, addRaw string
	keys := &cobra.Command{Use: "keys", Short: "manage kms-vault-plugin keys"}
	create := &cobra.Command{
		Use:   "create",
		Short: "create a key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if path == "" {
				return errors.New("required flag missing: path")
			}
			if chainsRaw == "" {
				return errors.New("required flag missing: chains")
			}
			c, err := st.client(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			chains, err := types.ParseChains(strings.Split(chainsRaw, ","))
			if err != nil {
				return err
			}
			createChains := make([]string, len(chains))
			for i, chain := range chains {
				createChains[i] = string(chain)
			}
			if err := c.CreateKey(cmd.Context(), path, createChains); err != nil {
				return err
			}
			return printKeyInfo(cmd, c, path)
		},
	}
	updateChains := &cobra.Command{
		Use:   "update-chains",
		Short: "expand a key's signing chains",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if path == "" {
				return errors.New("required flag missing: path")
			}
			if addRaw == "" {
				return errors.New("required flag missing: add")
			}
			c, err := st.client(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			addChains, err := types.ParseChains(strings.Split(addRaw, ","))
			if err != nil {
				return err
			}
			chainStrings := make([]string, len(addChains))
			for i, chain := range addChains {
				chainStrings[i] = string(chain)
			}
			chains, err := c.UpdateKeyChains(cmd.Context(), path, chainStrings)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(chains)
		},
	}
	show := &cobra.Command{
		Use:   "show",
		Short: "show a key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if path == "" {
				return errors.New("required flag missing: path")
			}
			c, err := st.client(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return printKeyInfo(cmd, c, path)
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "list keys by prefix",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := st.client(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			ks, err := c.ListKeys(cmd.Context(), prefix)
			if err != nil {
				return err
			}
			for _, k := range ks {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), k)
			}
			return nil
		},
	}
	create.Flags().StringVar(&path, "path", "", "key path")
	create.Flags().StringVar(&chainsRaw, "chains", "", "comma-separated signing chains (evm,cosmos)")
	updateChains.Flags().StringVar(&path, "path", "", "key path")
	updateChains.Flags().StringVar(&addRaw, "add", "", "comma-separated signing chains to add (evm,cosmos)")
	show.Flags().StringVar(&path, "path", "", "key path")
	list.Flags().StringVar(&prefix, "prefix", "", "key path prefix (optional)")
	keys.AddCommand(create, updateChains, show, list)
	return keys
}

func printKeyInfo(cmd *cobra.Command, c *vault.Client, path string) error {
	rawChains, err := c.GetKeyChains(cmd.Context(), path)
	if err != nil {
		return err
	}
	// Canonicalize persisted chains (allowing an empty list for legacy keys) so
	// address derivation in keyinfo.For matches on ChainEVM/ChainCosmos even if
	// Vault returns non-canonical values (case, whitespace, duplicates).
	chains := []types.Chain{}
	if len(rawChains) > 0 {
		chains, err = types.ParseChains(rawChains)
		if err != nil {
			return err
		}
	}
	info, err := keyinfo.For(cmd.Context(), c, path, keyinfo.DefaultHRP, chains)
	if err != nil {
		if errors.Is(err, types.ErrNotFound) {
			return fmt.Errorf("key not found: %s", path)
		}
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(info)
}

func signCmd(st *cliState) *cobra.Command {
	sign := &cobra.Command{Use: "sign", Short: "sign payloads"}
	sign.AddCommand(signEVMCmd(st), signCosmosCmd(st))
	return sign
}

func signEVMCmd(st *cliState) *cobra.Command {
	var path, rawTx string
	var chainID int64
	cmd := &cobra.Command{
		Use:   "evm",
		Short: "sign EVM raw transaction",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if path == "" {
				return errors.New("required flag missing: path")
			}
			if rawTx == "" {
				return errors.New("required flag missing: raw-tx")
			}
			c, err := st.client(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			raw, err := hex.DecodeString(strings.TrimPrefix(rawTx, "0x"))
			if err != nil {
				return err
			}
			out, err := evmsigner.New(c).SignRawTx(cmd.Context(), path, big.NewInt(chainID), raw)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "0x%x\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "key path")
	cmd.Flags().Int64Var(&chainID, "chain-id", 1, "EVM chain ID")
	cmd.Flags().StringVar(&rawTx, "raw-tx", "", "raw transaction hex")
	return cmd
}

func signCosmosCmd(st *cliState) *cobra.Command {
	var path, hrp, mode, signDoc string
	cmd := &cobra.Command{
		Use:   "cosmos",
		Short: "sign Cosmos sign doc",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if path == "" {
				return errors.New("required flag missing: path")
			}
			c, err := st.client(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			signer := cosmossigner.New(c)
			var sig, pub []byte
			// Important: keep `err` in the outer scope. Previously the DIRECT
			// case shadowed `err` via the base64 decode local, and the
			// subsequent `sig, pub, err = signer.SignDirect(...)` wrote to
			// the inner copy — the post-switch nil check ignored signer
			// failures and the CLI printed an empty success. The decode error
			// gets its own local (decErr) so the outer `err` belongs to the
			// signer call alone.
			switch mode {
			case "DIRECT":
				doc, decErr := base64.StdEncoding.DecodeString(signDoc)
				if decErr != nil {
					return fmt.Errorf("decode sign-doc: %w", decErr)
				}
				sig, pub, err = signer.SignDirect(cmd.Context(), path, doc)
			case "AMINO_JSON":
				sig, pub, err = signer.SignAmino(cmd.Context(), path, []byte(signDoc))
			default:
				return errors.New("unsupported sign_mode")
			}
			if err != nil {
				return fmt.Errorf("sign cosmos: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "signature: %s\npub_key: %s\n",
				base64.StdEncoding.EncodeToString(sig),
				base64.StdEncoding.EncodeToString(pub),
			)
			if addr, derr := cosmossigner.DeriveCosmosAddressFromCompressed(pub, hrp); derr == nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cosmos_address: %s\n", addr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "key path")
	cmd.Flags().StringVar(&hrp, "hrp", "cosmos", "bech32 HRP")
	cmd.Flags().StringVar(&mode, "mode", "DIRECT", "DIRECT or AMINO_JSON")
	cmd.Flags().StringVar(&signDoc, "sign-doc", "", "base64 sign doc or amino JSON")
	return cmd
}

func healthCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "check Vault health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := st.load(cmd.ErrOrStderr()); err != nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Config: INVALID (%s)\n", err)
				return fmt.Errorf("config error: %w", err)
			}
			c, err := vault.NewClient(st.cfg.Vault.Addr, vault.TokenAuthProvider{TokenValue: st.cfg.Vault.Token})
			if err != nil || c.Health() != nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Vault: UNREACHABLE (%s)\n", st.cfg.Vault.Addr)
				if err != nil {
					return err
				}
				return errors.New("vault unreachable")
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Vault: OK (%s)\n", st.cfg.Vault.Addr)
			return nil
		},
	}
}

func initLogger(level string, w io.Writer) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})))
}

func expandHome(path string) string {
	if path == "" || !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}
