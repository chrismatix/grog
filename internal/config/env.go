package config

import "github.com/spf13/viper"

// IsDebug Useful shorthands for looking up viper configs
func IsDebug() bool {
	return viper.GetString("log_level") == "debug"
}
