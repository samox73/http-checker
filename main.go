/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"log"

	"github.com/samox73/http-checker/cmd"
	"github.com/spf13/viper"
)

func main() {
	viper.AddConfigPath("./config")
	viper.AddConfigPath("/http-checker/configs")
	viper.SetConfigName("config")
	viper.SetConfigType("json")
	if err := viper.ReadInConfig(); err != nil {
		log.Fatal(err)
	}
	cmd.Execute()
}
