package path_handlers

import (
	"encoding/json"
	"fmt"
	vaultApi "github.com/hashicorp/vault/api"
	log "github.com/sirupsen/logrus"
	"github.com/starlingbank/vaultsmith/vault"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

/*
	SysAuth handles the creation/enabling of auth methods and policies, described in the
	configuration under sys.

	Currently it does not support templating, as I didn't see a need for it, but there's no reason
	it couldn't.
*/
type SysAuth struct {
	BaseHandler
	liveAuthMap       map[string]*vaultApi.AuthMount
	configuredAuthMap map[string]*vaultApi.AuthMount
}

func NewSysAuthHandler(client vault.Vault, config PathHandlerConfig) (*SysAuth, error) {
	// Build a map of currently active auth methods, so walkFile() can reference it
	liveAuthMap, err := client.ListAuth()
	if err != nil {
		return &SysAuth{}, err
	}

	// Create a mapping of configured auth methods, which we append to as we go,
	// so we can disable those that are missing at the end
	configuredAuthMap := make(map[string]*vaultApi.AuthMount)

	return &SysAuth{
		BaseHandler: BaseHandler{
			name:   "SysAuth",
			client: client,
			config: config,
			log: log.WithFields(log.Fields{
				"handler": "SysAuth",
			}),
		},
		liveAuthMap:       liveAuthMap,
		configuredAuthMap: configuredAuthMap,
	}, nil
}

func (sh *SysAuth) walkFile(path string, f os.FileInfo, err error) error {
	if f == nil {
		logger := sh.log.WithFields(log.Fields{"path": path, "error": err})
		logger.Debug("Path does not exist, skipping")
		return nil
	}
	if err != nil {
		return fmt.Errorf("error reading %s: %s", path, err)
	}
	// not doing anything with dirs
	if f.IsDir() {
		return nil
	}

	policyPath, err := apiPath(sh.config.DocumentPath, path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(policyPath, "sys/auth") {
		return fmt.Errorf("found file without sys/auth prefix: %s", policyPath)
	}

	fileContents, err := sh.readFile(path)
	if err != nil {
		return err
	}

	var enableOpts vaultApi.EnableAuthOptions
	err = json.Unmarshal([]byte(fileContents), &enableOpts)
	if err != nil {
		return fmt.Errorf("could not parse json from file %s: %s", path, err)
	}

	sysAuthPath := strings.TrimPrefix(policyPath, "sys/auth/") + "/"
	err = sh.ensureAuth(sysAuthPath, enableOpts)
	if err != nil {
		return fmt.Errorf("error while ensuring auth for path %s: %s", path, err)
	}

	return nil
}

func (sh *SysAuth) PutPoliciesFromDir(path string) error {
	err := filepath.Walk(path, sh.walkFile)
	if err != nil {
		return err
	}
	return sh.DisableUnconfiguredAuths()
}

// Ensure that this auth type is enabled and has the correct configuration
func (sh *SysAuth) ensureAuth(path string, enableOpts vaultApi.EnableAuthOptions) error {
	// we need to convert to AuthConfigOutput in order to compare with existing config
	var enableOptsAuthConfigOutput vaultApi.AuthConfigOutput
	enableOptsAuthConfigOutput, err := ConvertAuthConfig(enableOpts.Config)
	if err != nil {
		return err
	}

	authMount := vaultApi.AuthMount{
		Type:   enableOpts.Type,
		Config: enableOptsAuthConfigOutput,
	}
	sh.configuredAuthMap[path] = &authMount

	logger := sh.log.WithFields(log.Fields{
		"mount path":     path,
		"authMount.Type": enableOpts.Type,
	})

	if liveAuth, ok := sh.liveAuthMap[path]; ok {
		// If this path is present in our live config, we may not need to enable
		err, applied := sh.isConfigApplied(enableOpts.Config, liveAuth.Config)
		if err != nil {
			return fmt.Errorf(
				"could not determine whether configuration for auth mount %s was applied: %s",
				enableOpts.Type, err)
		}
		if applied {
			logger.Debugf("Auth mount configuration already applied")
			return nil
		}
	}
	logger.Infof("Applying auth mount")
	err = sh.client.EnableAuth(path, &enableOpts)
	if err != nil {
		return fmt.Errorf("could not enable auth %s: %s", path, err)
	}
	return nil
}

func (sh *SysAuth) DisableUnconfiguredAuths() error {
	// delete entries not in configured list
	for path, authMount := range sh.liveAuthMap {
		logger := log.WithFields(log.Fields{"authMount.Type": authMount.Type, "path": path})
		if _, ok := sh.configuredAuthMap[path]; ok {
			logger.Debugf("Not disabling auth mount, is configured")
			continue // present, do nothing
		} else if authMount.Type == "token" {
			continue // cannot be disabled, would give http 400 if attempted
		} else {
			logger.Infof("Disabling auth mount")
			err := sh.client.DisableAuth(path)
			if err != nil {
				return fmt.Errorf("failed to disable authMount at %s: %s", path, err)
			}
		}
	}
	return nil
}

// return true if the localConfig is reflected in remoteConfig, else false
func (sh *SysAuth) isConfigApplied(localConfig vaultApi.AuthConfigInput, remoteConfig vaultApi.AuthConfigOutput) (error, bool) {
	// AuthConfigInput uses different types for TTL, which need to be converted
	converted, err := ConvertAuthConfig(localConfig)
	if err != nil {
		return err, false
	}

	if reflect.DeepEqual(converted, remoteConfig) {
		return nil, true
	} else {
		return nil, false
	}
}

func (sh *SysAuth) Order() int {
	return sh.order
}

// convert AuthConfigInput type to AuthConfigOutput type
// A potential problem with this is that the transformation doesn't use the same code that Vault
// uses internally, so bugs are possible; but ParseDuration is pretty standard (and vault
// does use this same method)
func ConvertAuthConfig(input vaultApi.AuthConfigInput) (vaultApi.AuthConfigOutput, error) {
	var output vaultApi.AuthConfigOutput
	var dur time.Duration
	var err error

	var DefaultLeaseTTL int // was string

	if input.DefaultLeaseTTL != "" {
		dur, err = time.ParseDuration(input.DefaultLeaseTTL)
		if err != nil {
			return output, fmt.Errorf("could not parse DefaultLeaseTTL value %s as seconds: %s", input.DefaultLeaseTTL, err)
		}
		DefaultLeaseTTL = int(dur.Seconds())
	}

	var MaxLeaseTTL int // was string
	if input.MaxLeaseTTL != "" {
		dur, err = time.ParseDuration(input.MaxLeaseTTL)
		if err != nil {
			return output, fmt.Errorf("could not parse MaxLeaseTTL value %s as seconds: %s", input.MaxLeaseTTL, err)
		}
		MaxLeaseTTL = int(dur.Seconds())
	}

	output = vaultApi.AuthConfigOutput{
		DefaultLeaseTTL:           DefaultLeaseTTL,
		MaxLeaseTTL:               MaxLeaseTTL,
		PluginName:                input.PluginName,
		AuditNonHMACRequestKeys:   input.AuditNonHMACRequestKeys,
		AuditNonHMACResponseKeys:  input.AuditNonHMACResponseKeys,
		ListingVisibility:         input.ListingVisibility,
		PassthroughRequestHeaders: input.PassthroughRequestHeaders,
	}

	return output, nil
}
