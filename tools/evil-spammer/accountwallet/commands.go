package accountwallet

import (
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	"github.com/iotaledger/iota-core/tools/evil-spammer/models"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/builder"
)

func (a *AccountWallet) CreateAccount(params *CreateAccountParams) (iotago.AccountID, error) {
	implicitAccountOutput, privateKey, err := a.getFunds(params.Amount, iotago.AddressImplicitAccountCreation)
	if err != nil {
		return iotago.EmptyAccountID, ierrors.Wrap(err, "Failed to create account")
	}

	log.Infof("Implicit account created, ID: %s", implicitAccountOutput.OutputID.ToHex())
	implicitAccAddr := iotago.ImplicitAccountCreationAddressFromPubKey(privateKey.Public().(ed25519.PublicKey))
	addrKeys := iotago.NewAddressKeysForImplicitAccountCreationAddress(implicitAccAddr, privateKey)
	log.Infof("impl keys %v, addr %s", addrKeys.Keys, addrKeys.Address)

	implicitBlockIssuerKey := iotago.Ed25519PublicKeyHashBlockIssuerKeyFromImplicitAccountCreationAddress(implicitAccAddr)
	blockIssuerKeys := iotago.NewBlockIssuerKeys(implicitBlockIssuerKey)

	// transition from implicit to regular account
	accountOutput := builder.NewAccountOutputBuilder(addrKeys.Address, addrKeys.Address, implicitAccountOutput.Balance).
		Mana(implicitAccountOutput.OutputStruct.StoredMana()).
		AccountID(iotago.AccountIDFromOutputID(implicitAccountOutput.OutputID)).
		BlockIssuer(blockIssuerKeys, iotago.MaxSlotIndex).MustBuild()

	log.Infof("Created account %s with %d tokens\n", accountOutput.AccountID.ToHex(), params.Amount)

	_, err = a.createTransactionBuilder(implicitAccountOutput, implicitAccAddr, accountOutput)
	if err != nil {
		return iotago.EmptyAccountID, ierrors.Wrap(err, "Failed to create account")
	}
	//txBuilder.AllotRequiredManaAndStoreRemainingManaInOutput(txBuilder.CreationSlot(), rmc, f.account.ID(), remainderIndex)
	//
	//signedTx, err := txBuilder.Build(a.genesisHdWallet.AddressSigner())

	accountID := a.registerAccount(params.Alias, implicitAccountOutput.OutputID, a.latestUsedIndex, privateKey)

	fmt.Printf("Created account %s with %d tokens\n", accountID.ToHex(), params.Amount)

	return accountID, nil
}

func (a *AccountWallet) createTransactionBuilder(input *models.Output, address iotago.Address, accountOutput *iotago.AccountOutput) (*builder.TransactionBuilder, error) {
	currentTime := time.Now()
	currentSlot := a.client.LatestAPI().TimeProvider().SlotFromTime(currentTime)

	apiForSlot := a.client.APIForSlot(currentSlot)
	txBuilder := builder.NewTransactionBuilder(apiForSlot)

	txBuilder.AddInput(&builder.TxInput{
		UnlockTarget: address,
		InputID:      input.OutputID,
		Input:        input.OutputStruct,
	})
	txBuilder.AddOutput(accountOutput)
	txBuilder.SetCreationSlot(currentSlot)

	return txBuilder, nil
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
