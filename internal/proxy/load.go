package proxy

import (
	"github.com/TelegramMessenger/MTProxy/internal/cli"
	"github.com/TelegramMessenger/MTProxy/internal/config"
)

func LoadAndValidate(manager *config.Manager, opts cli.Options) (config.Snapshot, BootstrapResult, error) {
	snapshot, err := manager.Reload()
	if err != nil {
		return config.Snapshot{}, BootstrapResult{}, err
	}

	bootstrap, err := ValidateBootstrap(opts, snapshot.Config)
	if err != nil {
		return config.Snapshot{}, BootstrapResult{}, err
	}
	return snapshot, bootstrap, nil
}
