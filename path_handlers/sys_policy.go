package path_handlers

import (
	"os"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"github.com/starlingbank/vaultsmith/vault"
	"encoding/json"
)

/*
	SysPolicyHandler handles the creation/enabling of auth methods and policies, described in the
	configuration under sys
 */

// fixed policies that should not be deleted from vault under any circumstances
var fixedPolicies = map[string]bool {
	"root": true,
	"default": true,
}

type SysPolicyHandler struct {
	BaseHandler
	client					vault.Vault
	config					PathHandlerConfig
	livePolicyList			[]string
	configuredPolicyList	[]string
}

type SysPolicy struct {
	Name	string
	Policy	string `json:"policy"`
}

func NewSysPolicyHandler(c vault.Vault) (*SysPolicyHandler, error) {
	// Build a map of currently active auth methods, so walkFile() can reference it
	livePolicyList, err := c.ListPolicies()
	if err != nil {
		return &SysPolicyHandler{}, fmt.Errorf("error listing policies: %s", err)
	}

	return &SysPolicyHandler{
		client:              	c,
		livePolicyList:      	livePolicyList,
		configuredPolicyList:	[]string{},
	}, nil
}

func (sh *SysPolicyHandler) walkFile(path string, f os.FileInfo, err error) error {
	if f == nil {
		log.Printf("%q does not exist, skipping SysPolicy handler. Error was %q", path, err.Error())
		return nil
	}
	if err != nil {
		return fmt.Errorf("error reading %s: %s", path, err)
	}
	// not doing anything with dirs
	if f.IsDir() {
		return nil
	}

	log.Printf("Applying %s\n", path)
	fileContents, err := sh.readFile(path)
	if err != nil {
		return err
	}

	_, file := filepath.Split(path)
	var policy SysPolicy
	err = json.Unmarshal([]byte(fileContents), &policy)
	if err != nil {
		return fmt.Errorf("failed to parse json in %s: %s", path, err)
	}
	policy.Name = file

	err = sh.EnsurePolicy(policy)
	if err != nil {
		return fmt.Errorf("failed to apply policy from %s: %s", path, err)
	}

	return nil
}

func (sh *SysPolicyHandler) PutPoliciesFromDir(path string) error {
	err := filepath.Walk(path, sh.walkFile)
	if err != nil {
		return err
	}
	_, err = sh.RemoveUndeclaredPolicies()
	return err
}

func (sh *SysPolicyHandler) EnsurePolicy(policy SysPolicy) error {
	sh.configuredPolicyList = append(sh.configuredPolicyList, policy.Name)
	applied, err := sh.isPolicyApplied(policy)
	if err != nil {
		return err
	}
	if applied {
		log.Printf("Policy %s already applied", policy.Name)
		return nil
	}
	return sh.client.PutPolicy(policy.Name, policy.Policy)
}

func(sh *SysPolicyHandler) RemoveUndeclaredPolicies() (deleted []string, err error) {
	// only real reason to track the deleted policies is for testing as logs inform user
	for _, liveName := range sh.livePolicyList {
		if fixedPolicies[liveName] {
			// never want to delete default or root
			continue
		}

		// look for the policy in the configured list
		found := false
		for _, configuredName := range sh.configuredPolicyList {
			if liveName == configuredName {
				found = true // it's declared and should stay
				break
			}
		}

		if ! found {
			// not declared, delete
			log.Printf("Deleting policy %s", liveName)
			sh.client.DeletePolicy(liveName)
			deleted = append(deleted, liveName)
		}
	}
	return deleted, nil
}

// true if the policy exists on the server
func (sh *SysPolicyHandler) policyExists(policy SysPolicy) bool {
	//log.Printf("policy.Name: %s, policy list: %+v", policy.Name, sh.livePolicyList)
	for _, p := range sh.livePolicyList {
		if p == policy.Name	{
			return true
		}
	}

	return false
}

// true if the policy is applied on the server
func (sh *SysPolicyHandler) isPolicyApplied(policy SysPolicy) (bool, error) {
	if ! sh.policyExists(policy) {
		return false, nil
	}

	remotePolicy, err := sh.client.GetPolicy(policy.Name)
	if err != nil {
		return false, nil
	}

	if reflect.DeepEqual(policy.Policy, remotePolicy) {
		return true, nil
	} else {
		return false, nil
	}
}

func (sh *SysPolicyHandler) Order() int {
	return sh.order
}
