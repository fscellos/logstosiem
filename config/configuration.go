package configuration

import "github.com/spf13/viper"

const NOTSET string = "UNSET"

var Configuration GlobalConfiguration

// Controllers Config
type GlobalConfiguration struct {
	HarborConfig        HarborConfig `mapstructure:"harbor"`
	AdditionalTrustedCA string       `mapstructure:"additional_trusted_ca_dir"`
}

// Harbor Config
type HarborConfig struct {
	Url      string
	Login    string
	Password string
}

// Initialisation de la configuration
func init() {
	viper.SetConfigName("configuration")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	viper.SetDefault("webhook_cert_dir", "/tmp/k8s-webhook-server/serving-certs")
	viper.SetDefault("additional_trusted_ca_dir", NOTSET)

	if err := viper.ReadInConfig(); err != nil {
		return
	}

	err := viper.Unmarshal(&Configuration)
	if err != nil {
		return
	}
}
