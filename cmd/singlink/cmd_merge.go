package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/rw"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"

	"github.com/spf13/cobra"
)

var commandMerge = &cobra.Command{
	Use:   "merge <output-path>",
	Short: "Merge configurations",
	Run: func(cmd *cobra.Command, args []string) {
		err := merge(args[0])
		if err != nil {
			log.Fatal(err)
		}
	},
	Args: cobra.ExactArgs(1),
}

func init() {
	mainCommand.AddCommand(commandMerge)
}

func merge(outputPath string) error {
	mergedOptions, err := readConfigAndMerge()
	if err != nil {
		return err
	}
	containsSensitiveResources, err := mergePathResources(&mergedOptions)
	if err != nil {
		return err
	}
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(mergedOptions)
	if err != nil {
		return E.Cause(err, "encode config")
	}
	if existsContent, err := os.ReadFile(outputPath); err == nil && bytes.Equal(existsContent, buffer.Bytes()) {
		return nil
	}
	err = rw.MkdirParent(outputPath)
	if err != nil {
		return err
	}
	fileMode := os.FileMode(0o644)
	if containsSensitiveResources {
		fileMode = 0o600
	}
	err = os.WriteFile(outputPath, buffer.Bytes(), fileMode)
	if err != nil {
		return err
	}
	if containsSensitiveResources {
		err = os.Chmod(outputPath, fileMode)
		if err != nil {
			return err
		}
		os.Stderr.WriteString("warning: merged config contains inlined private key material\n")
	}
	outputPath, _ = filepath.Abs(outputPath)
	os.Stderr.WriteString(outputPath + "\n")
	return nil
}

func mergePathResources(options *option.Options) (bool, error) {
	var containsSensitiveResources bool
	for _, inbound := range options.Inbounds {
		if tlsOptions, containsTLSOptions := inbound.Options.(option.InboundTLSOptionsWrapper); containsTLSOptions {
			mergedOptions, containsSensitive := mergeTLSInboundOptions(tlsOptions.TakeInboundTLSOptions())
			containsSensitiveResources = containsSensitiveResources || containsSensitive
			tlsOptions.ReplaceInboundTLSOptions(mergedOptions)
		}
	}
	for _, outbound := range options.Outbounds {
		switch outbound.Type {
		case C.TypeSSH:
			containsSensitiveResources = mergeSSHOutboundOptions(outbound.Options.(*option.SSHOutboundOptions)) || containsSensitiveResources
		}
		if tlsOptions, containsTLSOptions := outbound.Options.(option.OutboundTLSOptionsWrapper); containsTLSOptions {
			mergedOptions, containsSensitive := mergeTLSOutboundOptions(tlsOptions.TakeOutboundTLSOptions())
			containsSensitiveResources = containsSensitiveResources || containsSensitive
			tlsOptions.ReplaceOutboundTLSOptions(mergedOptions)
		}
	}
	return containsSensitiveResources, nil
}

func mergeTLSInboundOptions(options *option.InboundTLSOptions) (*option.InboundTLSOptions, bool) {
	if options == nil {
		return nil, false
	}
	var containsSensitiveResources bool
	if options.CertificatePath != "" {
		if content, err := os.ReadFile(options.CertificatePath); err == nil {
			options.Certificate = trimStringArray(strings.Split(string(content), "\n"))
		}
	}
	if options.KeyPath != "" {
		if content, err := os.ReadFile(options.KeyPath); err == nil {
			options.Key = trimStringArray(strings.Split(string(content), "\n"))
			containsSensitiveResources = true
		}
	}
	if options.ECH != nil {
		if options.ECH.KeyPath != "" {
			if content, err := os.ReadFile(options.ECH.KeyPath); err == nil {
				options.ECH.Key = trimStringArray(strings.Split(string(content), "\n"))
				containsSensitiveResources = true
			}
		}
	}
	return options, containsSensitiveResources
}

func mergeTLSOutboundOptions(options *option.OutboundTLSOptions) (*option.OutboundTLSOptions, bool) {
	if options == nil {
		return nil, false
	}
	var containsSensitiveResources bool
	if options.CertificatePath != "" {
		if content, err := os.ReadFile(options.CertificatePath); err == nil {
			options.Certificate = trimStringArray(strings.Split(string(content), "\n"))
		}
	}
	if options.ClientCertificatePath != "" {
		if content, err := os.ReadFile(options.ClientCertificatePath); err == nil {
			options.ClientCertificate = trimStringArray(strings.Split(string(content), "\n"))
		}
	}
	if options.ClientKeyPath != "" {
		if content, err := os.ReadFile(options.ClientKeyPath); err == nil {
			options.ClientKey = trimStringArray(strings.Split(string(content), "\n"))
			containsSensitiveResources = true
		}
	}
	if options.ECH != nil {
		if options.ECH.ConfigPath != "" {
			if content, err := os.ReadFile(options.ECH.ConfigPath); err == nil {
				options.ECH.Config = trimStringArray(strings.Split(string(content), "\n"))
			}
		}
	}
	return options, containsSensitiveResources
}

func mergeSSHOutboundOptions(options *option.SSHOutboundOptions) bool {
	if options.PrivateKeyPath != "" {
		if content, err := os.ReadFile(os.ExpandEnv(options.PrivateKeyPath)); err == nil {
			options.PrivateKey = trimStringArray(strings.Split(string(content), "\n"))
			return true
		}
	}
	return false
}

func trimStringArray(array []string) []string {
	return common.Filter(array, func(it string) bool {
		return strings.TrimSpace(it) != ""
	})
}
