package helpers

import (
	"fmt"
	"math"
	"time"

	"github.com/pkg/errors"
	types "github.com/prysmaticlabs/eth2-types"
	iface "github.com/prysmaticlabs/prysm/beacon-chain/state/interface"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/timeutils"
)

// MaxSlotBuffer specifies the max buffer given to slots from
// incoming objects. (24 mins with mainnet spec)
const MaxSlotBuffer = uint64(1 << 7)

// SlotToEpoch returns the epoch number of the input slot.
//
// Spec pseudocode definition:
//  def compute_epoch_of_slot(slot: Slot) -> Epoch:
//    """
//    Return the epoch number of ``slot``.
//    """
//    return Epoch(slot // SLOTS_PER_EPOCH)
func SlotToEpoch(slot types.Slot) types.Epoch {
	return types.Epoch(slot.DivSlot(params.BeaconConfig().SlotsPerEpoch))
}

// CurrentEpoch returns the current epoch number calculated from
// the slot number stored in beacon state.
//
// Spec pseudocode definition:
//  def get_current_epoch(state: BeaconState) -> Epoch:
//    """
//    Return the current epoch.
//    """
//    return compute_epoch_of_slot(state.slot)
func CurrentEpoch(state iface.ReadOnlyBeaconState) types.Epoch {
	return SlotToEpoch(state.Slot())
}

// PrevEpoch returns the previous epoch number calculated from
// the slot number stored in beacon state. It also checks for
// underflow condition.
//
// Spec pseudocode definition:
//  def get_previous_epoch(state: BeaconState) -> Epoch:
//    """`
//    Return the previous epoch (unless the current epoch is ``GENESIS_EPOCH``).
//    """
//    current_epoch = get_current_epoch(state)
//    return GENESIS_EPOCH if current_epoch == GENESIS_EPOCH else Epoch(current_epoch - 1)
func PrevEpoch(state iface.ReadOnlyBeaconState) types.Epoch {
	currentEpoch := CurrentEpoch(state)
	if currentEpoch == 0 {
		return 0
	}
	return currentEpoch - 1
}

// NextEpoch returns the next epoch number calculated from
// the slot number stored in beacon state.
func NextEpoch(state iface.ReadOnlyBeaconState) types.Epoch {
	return SlotToEpoch(state.Slot()) + 1
}

// StartSlot returns the first slot number of the
// current epoch.
//
// Spec pseudocode definition:
//  def compute_start_slot_at_epoch(epoch: Epoch) -> Slot:
//    """
//    Return the start slot of ``epoch``.
//    """
//    return Slot(epoch * SLOTS_PER_EPOCH)
func StartSlot(epoch types.Epoch) (types.Slot, error) {
	slot, err := params.BeaconConfig().SlotsPerEpoch.SafeMul(uint64(epoch))
	if err != nil {
		return slot, errors.Errorf("start slot calculation overflows: %v", err)
	}
	return slot, nil
}

// EndSlot returns the last slot number of the
// current epoch.
func EndSlot(epoch types.Epoch) (types.Slot, error) {
	if epoch == math.MaxUint64 {
		return 0, errors.New("start slot calculation overflows")
	}
	slot, err := StartSlot(epoch + 1)
	if err != nil {
		return 0, err
	}
	return slot - 1, nil
}

// IsEpochStart returns true if the given slot number is an epoch starting slot
// number.
func IsEpochStart(slot types.Slot) bool {
	return slot%params.BeaconConfig().SlotsPerEpoch == 0
}

// IsEpochEnd returns true if the given slot number is an epoch ending slot
// number.
func IsEpochEnd(slot types.Slot) bool {
	return IsEpochStart(slot + 1)
}

// SlotsSinceEpochStarts returns number of slots since the start of the epoch.
func SlotsSinceEpochStarts(slot types.Slot) types.Slot {
	return slot % params.BeaconConfig().SlotsPerEpoch
}

// VerifySlotTime validates the input slot is not from the future.
func VerifySlotTime(genesisTime uint64, slot types.Slot, timeTolerance time.Duration) error {
	slotTime, err := SlotToTime(genesisTime, slot)
	if err != nil {
		return err
	}

	// Defensive check to ensure unreasonable slots are rejected
	// straight away.
	if err := ValidateSlotClock(slot, genesisTime); err != nil {
		return err
	}

	currentTime := timeutils.Now()
	diff := slotTime.Sub(currentTime)

	if diff > timeTolerance {
		return fmt.Errorf("could not process slot from the future, slot time %s > current time %s", slotTime, currentTime)
	}
	return nil
}

// SlotToTime takes the given slot and genesis time to determine the start time of the slot.
func SlotToTime(genesisTimeSec uint64, slot types.Slot) (time.Time, error) {
	timeSinceGenesis, err := slot.SafeMul(params.BeaconConfig().SecondsPerSlot)
	if err != nil {
		return time.Unix(0, 0), fmt.Errorf("slot (%d) is in the far distant future: %w", slot, err)
	}
	sTime, err := timeSinceGenesis.SafeAdd(genesisTimeSec)
	if err != nil {
		return time.Unix(0, 0), fmt.Errorf("slot (%d) is in the far distant future: %w", slot, err)
	}
	return time.Unix(int64(sTime), 0), nil
}

// SlotsSince computes the number of time slots that have occurred since the given timestamp.
func SlotsSince(time time.Time) types.Slot {
	return CurrentSlot(uint64(time.Unix()))
}

// CurrentSlot returns the current slot as determined by the local clock and
// provided genesis time.
func CurrentSlot(genesisTimeSec uint64) types.Slot {
	now := timeutils.Now().Unix()
	genesis := int64(genesisTimeSec)
	if now < genesis {
		return 0
	}
	return types.Slot(uint64(now-genesis) / params.BeaconConfig().SecondsPerSlot)
}

// ValidateSlotClock validates a provided slot against the local
// clock to ensure slots that are unreasonable are returned with
// an error.
func ValidateSlotClock(slot types.Slot, genesisTimeSec uint64) error {
	maxPossibleSlot := CurrentSlot(genesisTimeSec).Add(MaxSlotBuffer)
	// Defensive check to ensure that we only process slots up to a hard limit
	// from our local clock.
	if slot > maxPossibleSlot {
		return fmt.Errorf("slot %d > %d which exceeds max allowed value relative to the local clock", slot, maxPossibleSlot)
	}
	return nil
}

// RoundUpToNearestEpoch rounds up the provided slot value to the nearest epoch.
func RoundUpToNearestEpoch(slot types.Slot) types.Slot {
	if slot%params.BeaconConfig().SlotsPerEpoch != 0 {
		slot -= slot % params.BeaconConfig().SlotsPerEpoch
		slot += params.BeaconConfig().SlotsPerEpoch
	}
	return slot
}

// ComputeWeakSubjectivityPeriod returns the epoch of the latest weak subjectivity checkpoint for the active validator count and
// finalized epoch.
//
// Reference spec implementation:
// https://github.com/ethereum/eth2.0-specs/blob/weak-subjectivity-guide/specs/phase0/weak-subjectivity.md#calculating-the-weak-subjectivity-period
//  def compute_weak_subjectivity_period(state):
//    weak_subjectivity_period = MIN_VALIDATOR_WITHDRAWABILITY_DELAY
//    val_count = len(get_active_validator_indices(state, get_current_epoch(state)))
//    if val_count >= MIN_PER_EPOCH_CHURN_LIMIT * CHURN_LIMIT_QUOTIENT:
//        weak_subjectivity_period += SAFETY_DECAY*CHURN_LIMIT_QUOTIENT/(2*100)
//    else:
//        weak_subjectivity_period += SAFETY_DECAY*val_count/(2*100*MIN_PER_EPOCH_CHURN_LIMIT)
//    return weak_subjectivity_period
func ComputeWeakSubjectivityPeriod(valCount uint64) (types.Epoch, error) {
	wsp := params.BeaconConfig().MinValidatorWithdrawabilityDelay

	m := params.BeaconConfig().MinPerEpochChurnLimit
	q := params.BeaconConfig().ChurnLimitQuotient
	d := params.BeaconConfig().SafetyDecay
	if valCount >= m*q {
		v := d * q / (2 * 100)
		wsp += types.Epoch(v)
	} else {
		v, err := mathutil.Mul64(d, valCount)
		if err != nil {
			return 0, err
		}
		v /= 2 * 100 * m
		wsp += types.Epoch(v)
	}
	return wsp, nil
}

// VotingPeriodStartTime returns the current voting period's start time
// depending on the provided genesis and current slot.
func VotingPeriodStartTime(genesis uint64, slot types.Slot) uint64 {
	slots := params.BeaconConfig().SlotsPerEpoch.Mul(uint64(params.BeaconConfig().EpochsPerEth1VotingPeriod))
	startTime := uint64((slot - slot.ModSlot(slots)).Mul(params.BeaconConfig().SecondsPerSlot))
	return genesis + startTime
}
