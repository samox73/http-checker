/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"net/http"
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
		period, _ := cmd.Flags().GetInt("period")
		persist, _ := cmd.Flags().GetBool("persist")
		filename, _ := cmd.Flags().GetString("file")
		log := logger.New("period", period, "persist", persist)
		if filename != "" {
			log = log.With("filename", filename)
		}

		go func() { metrics.ServeProfilerAndMetrics(log, ":8080") }()
		if err != nil {
			return err
		}

		metrics := metrics.New()

		transport := &http.Transport{
			MaxIdleConns:        0,
			TLSHandshakeTimeout: 0,
			MaxIdleConnsPerHost: 1000,
			MaxConnsPerHost:     0,
			IdleConnTimeout:     0,
		}

		client := http.Client{
			Timeout:   time.Duration(period) * time.Second,
			Transport: transport,
		}
		httpChecker := httpchecker.New(client, metrics, *log, period, persist, filename)
		httpChecker.Run()
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
	runCmd.Flags().Bool("persist", false, "Whether to persist measurements in a csv file")
	runCmd.Flags().String("file", "", "Path to the file in which to write the csv results")
}
