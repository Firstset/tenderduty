package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"golang.org/x/term"

	td2 "github.com/firstset/tenderduty/v2/td2"
)

//go:embed example-config.yml
var defaultConfig []byte

func main() {
	var configFile, chainConfigDirectory, stateFile, encryptedFile, password string
	var dumpConfig, encryptConfig, decryptConfig, devMode bool
	flag.StringVar(&configFile, "f", "config.yml", "configuration file to use, can also be set with the ENV var 'CONFIG'")
	flag.StringVar(&encryptedFile, "encrypted-config", "config.yml.asc", "encrypted config file, only valid with -encrypt or -decrypt flag")
	flag.StringVar(&password, "password", "", "password to use for encrypting/decrypting the config, if unset will prompt, also can use ENV var 'PASSWORD'")
	flag.StringVar(&stateFile, "state", ".tenderduty-state.json", "file for storing state between restarts")
	flag.StringVar(&chainConfigDirectory, "cc", "chains.d", "directory containing additional chain specific configurations")
	flag.BoolVar(&dumpConfig, "example-config", false, "print the an example config.yml and exit")
	flag.BoolVar(&encryptConfig, "encrypt", false, "encrypt the file specified by -f to -encrypted-config")
	flag.BoolVar(&decryptConfig, "decrypt", false, "decrypt the file specified by -encrypted-config to -f")
	flag.BoolVar(&devMode, "devmode", false, "start up the web server in dev mode (reading files directly instead of embeding them)")
	flag.Parse()

	if dumpConfig {
		fmt.Println(string(defaultConfig))
		os.Exit(0)
	}

	if configFile == "config.yml" && os.Getenv("CONFIG") != "" {
		configFile = os.Getenv("CONFIG")
	}

	if os.Getenv("PASSWORD") != "" {
		password = os.Getenv("PASSWORD")
	}

	if encryptConfig || decryptConfig {
		if password == "" {
			fmt.Print("Please enter the encryption password: ")
			pass, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				slog.Error("failed to read password", "err", err)
				os.Exit(1)
			}
			fmt.Println("")
			password = string(pass)
			pass = nil
		}

		var e error
		if encryptConfig {
			e = td2.EncryptedConfig(configFile, encryptedFile, password, false)
		} else {
			e = td2.EncryptedConfig(configFile, encryptedFile, password, true)
		}
		if e != nil {
			slog.Error("failed to process encrypted config", "err", e)
			os.Exit(1)
		}
		os.Exit(0)
	}

	err := td2.Run(configFile, stateFile, chainConfigDirectory, &password, devMode)
	if err != nil {
		slog.Error("tenderduty exiting", "err", err)
	}
}
