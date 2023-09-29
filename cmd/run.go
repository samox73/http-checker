/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	httpchecker "github.com/samox73/http-checker/pkg/http-checker"
	"github.com/samox73/http-checker/pkg/logger"
	"github.com/samox73/http-checker/pkg/metrics"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Periodically perform http checks against a host.",
	Long:  `Periodically perform http checks against a host.`,
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		log := logger.New()
		go func() { metrics.ServeProfilerAndMetrics(log, ":8080") }()
		if err != nil {
			return err
		}
		urlFlags, _ := cmd.Flags().GetStringSlice("urls")
		for _, urlFlag := range urlFlags {
			if _, err := url.Parse(urlFlag); err != nil {
				return err
			}
		}
		period, _ := cmd.Flags().GetInt("period")
		persist, _ := cmd.Flags().GetBool("persist")
		file, _ := cmd.Flags().GetString("file")
		metrics := metrics.New()

		httpChecker := httpchecker.New(http.Client{Timeout: time.Duration(period) * time.Second}, metrics, *log)
		httpChecker.Run(urlFlags, period, persist, file)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// runCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	runCmd.Flags().IntP("period", "p", 30, "Number of seconds after which http checks should be performed")
	runCmd.Flags().StringSlice("urls", []string{}, "URLs against which to run http checks")
	if err := runCmd.MarkFlagRequired("urls"); err != nil {
		log.Fatalf("encountered error: %v", err)
	}
	runCmd.Flags().Bool("persist", false, "Whether to persist measurements in a csv file")
	runCmd.Flags().String("file", "measurements.csv", "Path to the file in which to write the csv results")
}
