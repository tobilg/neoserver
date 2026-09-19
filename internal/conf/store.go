package conf

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// LoadStore shares serve's file search and environment precedence without
// requiring unrelated HTTP/datasource settings to be valid for administration.
func LoadStore(configFile, pathOverride string) (Store, error) {
	v := viper.New()
	v.SetDefault("Store.Path", "./data/neoserver.db")
	v.SetDefault("Store.EncryptionKey", "")
	v.SetEnvPrefix(App.EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		v.SetConfigName(App.Name)
		v.SetConfigType("toml")
		for _, path := range []string{"./config", "/config", "/etc"} {
			v.AddConfigPath(path)
		}
	}
	if err := v.ReadInConfig(); err != nil {
		var missing viper.ConfigFileNotFoundError
		if configFile != "" || !errors.As(err, &missing) {
			return Store{}, fmt.Errorf("read config: %w", err)
		}
	}
	result := Store{Path: v.GetString("Store.Path"), EncryptionKey: v.GetString("Store.EncryptionKey")}
	if result.EncryptionKey == "" {
		result.EncryptionKey = os.Getenv("NEOSRV_STORE_KEY")
	}
	if pathOverride != "" {
		result.Path = pathOverride
	}
	return result, nil
}
