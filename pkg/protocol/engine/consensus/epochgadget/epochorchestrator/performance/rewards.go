package performance

import (
	"github.com/cockroachdb/errors"

	"github.com/iotaledger/hive.go/ads"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/lo"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (m *Tracker) RewardsRoot(epochIndex iotago.EpochIndex) iotago.Identifier {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return iotago.Identifier(ads.NewMap[iotago.AccountID, PoolRewards](m.rewardsStorage(epochIndex)).Root())
}

func (m *Tracker) ValidatorReward(validatorID iotago.AccountID, stakeAmount uint64, epochStart, epochEnd iotago.EpochIndex) (validatorReward uint64, err error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for epochIndex := epochStart; epochIndex <= epochEnd; epochIndex++ {
		rewardsForAccountInEpoch, exists := m.rewardsForAccount(validatorID, epochIndex)
		if !exists {
			continue
		}

		poolStats, err := m.poolStats(epochIndex)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to get pool stats for epoch %d", epochIndex)
		}

		unDecayedEpochRewards := uint64(rewardsForAccountInEpoch.FixedCost) +
			((poolStats.ProfitMargin * uint64(rewardsForAccountInEpoch.PoolRewards)) >> 8) +
			((((1<<8)-poolStats.ProfitMargin)*uint64(rewardsForAccountInEpoch.PoolRewards))>>8)*
				uint64(stakeAmount)/
				uint64(rewardsForAccountInEpoch.PoolStake)

		decayedEpochRewards, err := m.decayProvider.RewardsWithDecay(iotago.Mana(unDecayedEpochRewards), epochIndex, epochEnd)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to calculate rewards with decay for epoch %d", epochIndex)
		}
		validatorReward += uint64(decayedEpochRewards)
	}

	return validatorReward, nil
}

func (m *Tracker) DelegatorReward(validatorID iotago.AccountID, delegatedAmount uint64, epochStart, epochEnd iotago.EpochIndex) (delegatorsReward uint64, err error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for epochIndex := epochStart; epochIndex <= epochEnd; epochIndex++ {
		rewardsForAccountInEpoch, exists := m.rewardsForAccount(validatorID, epochIndex)
		if !exists {
			continue
		}

		poolStats, err := m.poolStats(epochIndex)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to get pool stats for epoch %d", epochIndex)
		}

		unDecayedEpochRewards := ((((1 << 8) - poolStats.ProfitMargin) * uint64(rewardsForAccountInEpoch.PoolRewards)) >> 8) *
			uint64(delegatedAmount) /
			uint64(rewardsForAccountInEpoch.PoolStake)

		decayedEpochRewards, err := m.decayProvider.RewardsWithDecay(iotago.Mana(unDecayedEpochRewards), epochIndex, epochEnd)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to calculate rewards with decay for epoch %d", epochIndex)
		}

		delegatorsReward += uint64(decayedEpochRewards)
	}

	return delegatorsReward, nil
}

func (m *Tracker) rewardsStorage(epochIndex iotago.EpochIndex) kvstore.KVStore {
	return lo.PanicOnErr(m.rewardBaseStore.WithExtendedRealm(epochIndex.Bytes()))
}

func (m *Tracker) rewardsForAccount(accountID iotago.AccountID, epochIndex iotago.EpochIndex) (rewardsForAccount *PoolRewards, exists bool) {
	return ads.NewMap[iotago.AccountID, PoolRewards](m.rewardsStorage(epochIndex)).Get(accountID)
}

func calculateProfitMargin(totalValidatorsStake, totalPoolStake iotago.BaseToken) uint64 {
	return (1 << 8) * uint64(totalValidatorsStake) / (uint64(totalValidatorsStake) + uint64(totalPoolStake))
}

func poolReward(totalValidatorsStake, totalStake iotago.BaseToken, profitMargin uint64, fixedCosts iotago.Mana, performanceFactor uint64) iotago.Mana {
	// TODO: decay is calculated per epoch now, so do we need to calculate the rewards for each slot of the epoch?
	return iotago.Mana(uint64(totalValidatorsStake) * performanceFactor)
}
