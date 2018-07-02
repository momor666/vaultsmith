package internal

import (
	"github.com/stretchr/testify/suite"
	"testing"
	"log"
	"github.com/starlingbank/vaultsmith/mocks"
	vaultApi "github.com/hashicorp/vault/api"
)

type SysHandlerTestSuite struct {
	suite.Suite
	handler *SysHandler
}

func (suite *SysHandlerTestSuite) SetupTest() {
	client := &mocks.MockVaultsmithClient{}
	sh, err := NewSysHandler(client, "")
	if err != nil {
		log.Fatalf("failed to create SysHandler (using mock client): %s", err)
	}
	suite.handler = sh
}

func (suite *SysHandlerTestSuite) TearDownTest() {

}

func (suite *SysHandlerTestSuite) TestEnsureAuth() {
	enableOpts := vaultApi.EnableAuthOptions{ }
	err := suite.handler.EnsureAuth("foo", enableOpts)
	if err != nil {
		log.Fatalf("Error calling EnsureAuth: %s", err)
	}
}

func TestSysHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(SysHandlerTestSuite))
}
