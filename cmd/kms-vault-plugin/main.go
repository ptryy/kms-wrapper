package main

import (
	"os"

	"github.com/hashicorp/go-hclog"
	vaultapi "github.com/hashicorp/vault/api"
	vaultplugin "github.com/hashicorp/vault/sdk/plugin"

	kmsplugin "github.com/ryan-truong/kms-wrapper/internal/plugin"
)

func main() {
	apiClientMeta := &vaultapi.PluginAPIClientMeta{}
	flags := apiClientMeta.FlagSet()
	if err := flags.Parse(os.Args[1:]); err != nil {
		hclog.Default().Error("plugin flag parse failed", "error", err)
		os.Exit(1)
	}

	tlsConfig := apiClientMeta.GetTLSConfig()
	tlsProviderFunc := vaultapi.VaultPluginTLSProvider(tlsConfig)

	if err := vaultplugin.ServeMultiplex(&vaultplugin.ServeOpts{
		BackendFactoryFunc: kmsplugin.Factory,
		TLSProviderFunc:    tlsProviderFunc,
	}); err != nil {
		hclog.Default().Error("plugin shutting down", "error", err)
		os.Exit(1)
	}
}
