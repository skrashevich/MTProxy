package proxy

import (
	"fmt"

	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
)

type BootstrapResult struct {
	Warnings []string
}

func ValidateBootstrap(opts cli.Options, cfg config.Config) (BootstrapResult, error) {
	var res BootstrapResult

	if !cfg.HaveProxy {
		return res, fmt.Errorf("no MTProto next proxy servers defined to forward queries to")
	}

	if len(opts.Domains) > 0 {
		if opts.Workers > 0 {
			res.Warnings = append(res.Warnings, "It is recommended to not use workers with TLS-transport")
		}
		if len(opts.Secrets) == 0 {
			return res, fmt.Errorf("You must specify at least one mtproto-secret to use when using TLS-transport")
		}
	}

	return res, nil
}
