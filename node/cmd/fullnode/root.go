/*
	Copywrite 2017-2018 OneLedger

	Setup the root command structure for the cli
*/
package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
)

var RootCmd = &cobra.Command{
	Use:   "fullnode",
	Short: "fullnode",
	Long:  "A full node",
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

// Initialize Cobra
func init() {
	cobra.OnInitialize(environment)
	// RootCmd.PersistentFlags().StringVarP(&variable, "c", "command", "description")
}

// Initialize Viper
func environment() {
	viper.AutomaticEnv()
}
