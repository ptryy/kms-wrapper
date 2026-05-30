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
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ryan-truong/kms-wrapper/internal/config"
	"github.com/ryan-truong/kms-wrapper/internal/gateway"
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
		Short: "Vault Transit-backed multi-chain signing gateway",
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
			fmt.Fprintln(warnOut, msg)
		}
	})
	if err != nil {
		return err
	}
	if s.logLevel != "" {
		cfg.LogLevel = s.logLevel
	}
	s.cfg = cfg
	return cfg.ValidateRuntime()
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
			initLogger(st.cfg.LogLevel, cmd.ErrOrStderr())
			c.StartRenewal(context.Background())
			slog.Info("starting gateway", "addr", st.cfg.Gateway.Addr)
			return gateway.New(st.cfg, c, evmsigner.New(c), cosmossigner.New(c)).ListenAndServe()
		},
	}
}

func keysCmd(st *cliState) *cobra.Command {
	var path, prefix string
	keys := &cobra.Command{Use: "keys", Short: "manage Vault Transit keys"}
	create := &cobra.Command{
		Use:   "create",
		Short: "create a key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if path == "" {
				return errors.New("required flag missing: path")
			}
			c, err := st.client(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if err := c.CreateKey(cmd.Context(), path); err != nil {
				return err
			}
			return printKeyInfo(cmd, c, path)
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
				fmt.Fprintln(cmd.OutOrStdout(), k)
			}
			return nil
		},
	}
	create.Flags().StringVar(&path, "path", "", "key path")
	show.Flags().StringVar(&path, "path", "", "key path")
	list.Flags().StringVar(&prefix, "prefix", "", "key path prefix (optional)")
	keys.AddCommand(create, show, list)
	return keys
}

func printKeyInfo(cmd *cobra.Command, c *vault.Client, path string) error {
	pub, err := c.GetPublicKey(cmd.Context(), path)
	if err != nil {
		if errors.Is(err, types.ErrNotFound) {
			return fmt.Errorf("key not found: %s", path)
		}
		return err
	}
	evmAddr, err := evmsigner.DeriveEVMAddress(pub)
	if err != nil {
		return err
	}
	cosmosAddr, err := cosmossigner.DeriveCosmosAddress(pub, "cosmos")
	if err != nil {
		return err
	}
	info := types.KeyInfo{Path: path, PublicKeyHex: hex.EncodeToString(pub), EVMAddress: evmAddr, CosmosAddress: cosmosAddr}
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
			fmt.Fprintf(cmd.OutOrStdout(), "0x%x\n", out)
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
			switch mode {
			case "DIRECT":
				doc, err := base64.StdEncoding.DecodeString(signDoc)
				if err != nil {
					return err
				}
				sig, pub, err = signer.SignDirect(cmd.Context(), path, doc)
			case "AMINO_JSON":
				sig, pub, err = signer.SignAmino(cmd.Context(), path, []byte(signDoc))
			default:
				return errors.New("unsupported sign_mode")
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "signature: %s\npub_key: %s\n",
				base64.StdEncoding.EncodeToString(sig),
				base64.StdEncoding.EncodeToString(pub),
			)
			if addr, err := cosmossigner.DeriveCosmosAddressFromCompressed(pub, hrp); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "cosmos_address: %s\n", addr)
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
				fmt.Fprintf(cmd.OutOrStdout(), "Config: INVALID (%s)\n", err)
				return fmt.Errorf("config error: %w", err)
			}
			c, err := vault.NewClient(st.cfg.Vault.Addr, vault.TokenAuthProvider{TokenValue: st.cfg.Vault.Token})
			if err != nil || c.Health() != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Vault: UNREACHABLE (%s)\n", st.cfg.Vault.Addr)
				if err != nil {
					return err
				}
				return errors.New("vault unreachable")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Vault: OK (%s)\n", st.cfg.Vault.Addr)
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
