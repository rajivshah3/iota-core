package accountwallet

import (
	"crypto/ed25519"
	"fmt"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (a *AccountWallet) CreateAccount(params *CreateAccountParams) (iotago.AccountID, error) {
	implicitAccountOutput, privateKey, err := a.getFunds(params.Amount, iotago.AddressImplicitAccountCreation)
	if err != nil {
		return iotago.EmptyAccountID, ierrors.Wrap(err, "Failed to create account")
	}

	accountID := a.registerAccount(params.Alias, implicitAccountOutput.OutputID, a.latestUsedIndex, privateKey)

	fmt.Printf("Created account %s with %d tokens\n", accountID.ToHex(), params.Amount)

	return accountID, nil
}

func (a *AccountWallet) getAddress(addressType iotago.AddressType) (iotago.DirectUnlockableAddress, ed25519.PrivateKey, uint64) {
	hdWallet := mock.NewHDWallet("", a.seed[:], a.latestUsedIndex+1)
	privKey, _ := hdWallet.KeyPair()
	receiverAddr := hdWallet.Address(addressType)

	a.latestUsedIndex++

	return receiverAddr, privKey, a.latestUsedIndex - 1
}

func (a *AccountWallet) DestroyAccount(params *DestroyAccountParams) error {
	return a.destroyAccount(params.AccountAlias)
}

func (a *AccountWallet) ListAccount() error {
	fmt.Printf("%-10s \t%-33s\n\n", "Alias", "AccountID")
	for _, accData := range a.accountsAliases {
		fmt.Printf("%-10s \t", accData.Alias)
		fmt.Printf("%-33s ", accData.Account.ID().ToHex())
		fmt.Printf("\n")
	}

	return nil
}

func (a *AccountWallet) AllotToAccount(params *AllotAccountParams) error {
	return nil
}
