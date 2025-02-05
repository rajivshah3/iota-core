package main

import (
	"time"

	"github.com/iotaledger/iota-core/tools/evil-spammer/accountwallet"
	"github.com/iotaledger/iota-core/tools/evil-spammer/evilwallet"
	"github.com/iotaledger/iota-core/tools/evil-spammer/programs"
	"github.com/iotaledger/iota-core/tools/evil-spammer/spammer"
)

// Nodes used during the test, use at least two nodes to be able to doublespend.
var (
	// urls = []string{"http://bootstrap-01.feature.shimmer.iota.cafe:8080", "http://vanilla-01.feature.shimmer.iota.cafe:8080", "http://drng-01.feature.shimmer.iota.cafe:8080"}
	urls = []string{"http://localhost:8080"} //, "http://localhost:8090", "http://localhost:8070", "http://localhost:8040"}
	//urls = []string{}
)

var (
	Script     = "basic"
	Subcommand = ""

	customSpamParams = programs.CustomSpamParams{
		ClientURLs:            urls,
		SpamTypes:             []string{spammer.TypeBlock},
		Rates:                 []int{1},
		Durations:             []time.Duration{time.Second * 20},
		BlkToBeSent:           []int{0},
		TimeUnit:              time.Second,
		DelayBetweenConflicts: 0,
		NSpend:                2,
		Scenario:              evilwallet.Scenario1(),
		DeepSpam:              false,
		EnableRateSetter:      false,
		AccountAlias:          accountwallet.FaucetAccountAlias,
	}

	quickTestParams = programs.QuickTestParams{
		ClientURLs:            urls,
		Rate:                  100,
		Duration:              time.Second * 30,
		TimeUnit:              time.Second,
		DelayBetweenConflicts: 0,
		EnableRateSetter:      false,
	}

	accountsSubcommandsFlags []accountwallet.AccountSubcommands

	//nolint:godot
	// commitmentsSpamParams = CommitmentsSpamParams{
	// 	Rate:           1,
	// 	Duration:       time.Second * 20,
	// 	TimeUnit:       time.Second,
	// 	NetworkAlias:   "docker",
	// 	SpammerAlias:   "validator-1",
	// 	ValidAlias:     accountwallet.FaucetAccountAlias,
	// 	CommitmentType: "latest",
	// 	ForkAfter:      10,
	// }
)
