package alien

import "math/big"

const (
	checkpointInterval = 360 //360        // About N hours if config.period is N

	secondsPerDay                    = 24 * 60 * 60 // Number of seconds for one day
	accumulateFlowRewardInterval     = 2 * 60 * 60  // accumulate flow reward interval every day

	paySignerRewardInterval    = 0               // pay singer reward  interval every day
	payFlowRewardInterval      = 2*60*60 + 30*60 //  pay flow reward  interval every day
	payBandwidthRewardInterval = 1*60*60 + 30*60 //  pay bandwidth reward  interval every day

	signerPledgeLockParamPeriod    = 180 * 24 * 60 * 60
	signerPledgeLockParamRlsPeriod = 0
	signerPledgeLockParamInterval  = 0

	flowPledgeLockParamPeriod    = 180 * 24 * 60 * 60
	flowPledgeLockParamRlsPeriod = 0
	flowPledgeLockParamInterval  = 0

	rewardLockParamPeriod         = 30 * 24 * 60 * 60
	rewardLockParamRlsPeriod      = 180 * 24 * 60 * 60
	rewardLockParamInterval       = 24 * 60 * 60
	maxCandidateMiner             = 500 //	The maximum number of candidate nodes participating in each election is 500
	electionPartitionThreshold    = 36  //Election partition threshold
	signFixBlockNumber            = 326630
	grantProfitOneTimeBlockNumber = 372842
	lockSimplifyEffectBlocknumber = 380182

	lockMergeNumber                      = 397000
	tallyRevenueEffectBlockNumber        = 516460
	SigerQueueFixBlockNumber             = 591790
	SigerElectNewEffectBlockNumber       = 661874
	MinerUpdateStateFixBlockNumber       = 757054
	TallyPunishdProcessEffectBlockNumber = 757114
	TallyPunishdFixBlockNumber           = 774331
	StorageEffectBlockNumber             = 834261
	//storage
	storageVerificationCheck = 1 * 60 * 60 //return funds where contract expiration
	rentRenewalExpires       = 95
	rentFailToRescind        = 10
	maxStgVerContinueDayFail = 30 //storage Verification failed and failed for 7 consecutive days

	SPledgeRevertFixBlockNumber = 894333
	AdjustSPRBlockNumber        = 974868 //Adjust calc StoragePledgeReward

	storageVerifyNewEffectNumber         = 1073447
	storagePledgeTmpVerifyEffectNumber   = 1103667
	StorageChBwEffectNumber              = 1170700
	storagePledgeTmpVerifyEffectNumberV2 = 1240413
	PledgeRevertLockEffectNumber         = 1495994
	payPOSPGRedeemInterval               = 1*60*60 + 40*60 //  pay bandwidth reward  interval every day
	StoragePledgeOptEffectNumber         = 1608207
	FixLeaseCapacityNumber               = 1660014
	PosrIncentiveEffectNumber            = 1744720
	PosrExitNewRuleEffectNumber          = 1953447
	PosrNewCalEffectNumber               = 2306412
	PosNewEffectNumber                   = 2558551
	payPOSExitInterval                   = 1*60*60 + 50*60 //  pay bandwidth reward  interval every day
	checkPOSAutoExit                     = 1*60*60 + 60*60
	PosLastPunishFixNumber               = 2566675
	PosAutoExitPunishChangeNumber        = 2755395
	GrantEffectNumber                    = 3713619
	PoCrsAccCalNumber                    = 3946692
	initStorageManagerNumber             = 5173314
	utgLockRewardInterval                = 30 //Lock and release every 30 days
	accumulateRewardLockInterval   = 2*60*60 +10*10
	paySpReWardInterval                    = 2*60*60 + 10*50
	paySpEntrustRewardInterval             = 2*60*60 + 10*60
	paySTPEntrustExitInterval            = 2*60*60 + 10*70
	paySpExitInterval                    = 2*60*60 + 10*80
	paySpEntrustExitInterval             = 2*60*60 + 10*90
	paySTPEntrustInterval                = 2*60*60 + 10*100
)

var (
	minCndPledgeBalance        = new(big.Int).Mul(big.NewInt(1e+18), big.NewInt(20)) // candidate pledge balance
	minSignerLockBalance       = new(big.Int).Mul(big.NewInt(1e+18), big.NewInt(0))  // signer reward lock balance
	minFlwLockBalance          = new(big.Int).Mul(big.NewInt(1e+18), big.NewInt(0))  // flow reward lock balance
	minBandwidthLockBalance    = new(big.Int).Mul(big.NewInt(1e+18), big.NewInt(0))  // bandwidth reward lock balance
	baseStoragePrice           = new(big.Int).Mul(big.NewInt(1e+14), big.NewInt(5))
	clearSignNumberPerid       = uint64(60480)
	storagePledgeIndex         = big.NewInt(1)
	defaultLeaseExpires        = big.NewInt(1)
	minimumRentDay             = big.NewInt(30)
	novalidPktime              = uint64(7)
	novalidVfPktime            = uint64(30)
	maximumRentDay             = big.NewInt(360)
	posCommitPeriod            = big.NewInt(365 + 365/2) //1.5 year
	posBeyondCommitPeriod      = big.NewInt(30)          //30 day
	posWithinCommitPeriod      = big.NewInt(30)          //30 day
	posMaxMainCandidateNum     = 11
	posCandidateAvgRate        = big.NewInt(70) //70%
	minCndEntrustPledgeBalance = new(big.Int).Mul(big.NewInt(1e+18), big.NewInt(1))
	maxPosContinueDayFail      = uint64(30)
	posDistributionDefaultRate = big.NewInt(10000)
)

func (a *Alien) blockPerDay() uint64 {
	return secondsPerDay / a.config.Period
}

func (a *Alien) blockPaySignerRewardInterval() uint64 {
	return paySignerRewardInterval / a.config.Period
}

func (a *Alien) blockPayFlowRewardInterval() uint64 {
	return payFlowRewardInterval / a.config.Period
}

func isPayBandWidthRewards(number uint64, period uint64) bool {
	if isGEInitStorageManagerNumber(number) {
		block := payBandwidthRewardInterval / period
		blockPerDay := secondsPerDay / period
		return number%(utgLockRewardInterval*blockPerDay) == block
	}
	block := payBandwidthRewardInterval / period
	blockPerDay := secondsPerDay / period
	return block == number%blockPerDay && block != number
}

func isPayFlowRewards(number uint64, period uint64) bool {
	if isGEInitStorageManagerNumber(number) {
		block := payFlowRewardInterval / period
		blockPerDay := secondsPerDay / period
		return number%(utgLockRewardInterval*blockPerDay) == block
	}
	block := payFlowRewardInterval / period
	blockPerDay := secondsPerDay / period
	return block == number%blockPerDay && block != number
}
func isPaySignerRewards(number uint64, period uint64) bool {
	if isGEInitStorageManagerNumber(number) {
		block := paySignerRewardInterval / period
		blockPerDay := secondsPerDay / period
		return number%(utgLockRewardInterval*blockPerDay) == block
	}
	block := paySignerRewardInterval / period
	blockPerDay := secondsPerDay / period
	return block == number%blockPerDay && block != number
}
func islockSimplifyEffectBlocknumber(number uint64) bool {
	return number >= lockSimplifyEffectBlocknumber
}

func isStorageVerificationCheck(number uint64, period uint64) bool {
	block := storageVerificationCheck / period
	blockPerDay := secondsPerDay / period
	return block == number%blockPerDay && block != number
}
func isPayPosPledgeExit(number uint64, period uint64) bool {
	if isGEInitStorageManagerNumber(number) {
		block := payPOSPGRedeemInterval / period
		blockPerDay := secondsPerDay / period
		return number%(utgLockRewardInterval*blockPerDay) == block
	}
	if number < PledgeRevertLockEffectNumber {
		return false
	}
	block := payPOSPGRedeemInterval / period
	blockPerDay := secondsPerDay / period
	return block == number%blockPerDay && block != number
}

func isPayPosExit(number uint64, period uint64) bool {
	if isGEInitStorageManagerNumber(number) {
		block := payPOSExitInterval / period
		blockPerDay := secondsPerDay / period
		return number%(utgLockRewardInterval*blockPerDay) == block
	}
	if number < PosNewEffectNumber {
		return false
	}
	block := payPOSExitInterval / period
	blockPerDay := secondsPerDay / period
	return block == number%blockPerDay && block != number
}

func isCheckPOSAutoExit(number uint64, period uint64) bool {
	if number < PosNewEffectNumber {
		return false
	}
	block := checkPOSAutoExit / period
	blockPerDay := secondsPerDay / period
	return block == number%blockPerDay && block != number
}

func (a *Alien) notVerifyPkHeader(number uint64) bool {
	r1 := number >= storagePledgeTmpVerifyEffectNumber && number <= storagePledgeTmpVerifyEffectNumber+a.blockPerDay()*novalidPktime
	r2 := number >= storagePledgeTmpVerifyEffectNumberV2 && number <= storagePledgeTmpVerifyEffectNumberV2+a.blockPerDay()*novalidVfPktime
	return r1 || r2
}
func (a *Alien) isEffectPayPledge(number uint64) bool {
	return number >= StoragePledgeOptEffectNumber && number <= StoragePledgeOptEffectNumber+BandwidthMakeupPunishDay*a.blockPerDay()
}
func (a *Alien) changeBandwidthEnable(number uint64) bool {
	r1 := number >= StorageChBwEffectNumber && number < StoragePledgeOptEffectNumber
	r2 := number >= PosrIncentiveEffectNumber
	return r1 || r2
}
func isGTIncentiveEffect(number uint64) bool {
	return number > PosrIncentiveEffectNumber
}

func isFixLeaseCapacity(number uint64) bool {
	return number == FixLeaseCapacityNumber
}

func isGTPOSRNewCalEffect(number uint64) bool {
	return number > PosrNewCalEffectNumber
}
func isGEPOSNewEffect(number uint64) bool {
	return number >= PosNewEffectNumber
}

func isLtPosAutoExitPunishChange(number uint64) bool {
	return number < PosAutoExitPunishChangeNumber
}

func isGEPosAutoExitPunishChange(number uint64) bool {
	return number >= PosAutoExitPunishChangeNumber
}

func isLtGrantEffectNumber(number uint64) bool {
	return number < GrantEffectNumber
}

func isGEGrantEffectNumber(number uint64) bool {
	return number >= GrantEffectNumber
}

func isGEPoCrsAccCalNumber(number uint64) bool {
	return number >= PoCrsAccCalNumber
}
func isGEInitStorageManagerNumber(number uint64) bool {
	return number >= initStorageManagerNumber
}
func isPaySTPEntrustExit(number uint64, period uint64) bool {
	if number < initStorageManagerNumber {
		return false
	}
	block := paySTPEntrustExitInterval / period
	blockPerDay := secondsPerDay / period
	return number%(utgLockRewardInterval*blockPerDay) == block
}
func isPaySTPEntrust(number uint64, period uint64) bool {
	if number < initStorageManagerNumber {
		return false
	}
	block := paySTPEntrustInterval / period
	blockPerDay := secondsPerDay / period
	return number%(utgLockRewardInterval*blockPerDay) == block
}
func isSpVerificationCheck(number uint64, period uint64) bool {
	block := storageVerificationCheck/ period
	blockPerDay := secondsPerDay / period
	return block- 2 == number%blockPerDay && block != number
}
func isLtInitStorageManagerNumber(number uint64) bool {
	return number < initStorageManagerNumber
}
func isLockRewardNumber(number uint64, period uint64) bool {
	lockBlockInteval := (utgLockRewardInterval * secondsPerDay) / period
	block := accumulateRewardLockInterval / period
	return number > initStorageManagerNumber && number%lockBlockInteval == block
}
func isPaySpExit(number uint64, period uint64) bool {
	if number < initStorageManagerNumber {
		return false
	}
	blockPerDay := secondsPerDay / period
	block := paySpExitInterval / period
	return  number%(utgLockRewardInterval*blockPerDay) == block
}
func isPaySpEntrustExit(number uint64, period uint64) bool {
	if number < initStorageManagerNumber {
		return false
	}
	blockPerDay := secondsPerDay / period
	block := paySpEntrustExitInterval / period
	return  number%(utgLockRewardInterval*blockPerDay) == block
}
func isPaySpEntrustReWard(number uint64, period uint64) bool {
	if number < initStorageManagerNumber {
		return false
	}
	blockPerDay := secondsPerDay / period
	block := paySpEntrustRewardInterval / period
	return  number%(utgLockRewardInterval*blockPerDay) == block
}
func isPaySpReWard(number uint64, period uint64) bool {
	if number < initStorageManagerNumber {
		return false
	}
	blockPerDay := secondsPerDay / period
	block := paySpReWardInterval / period
	return  number%(utgLockRewardInterval*blockPerDay) == block
}
func isSpDelExit(number uint64, period uint64) bool {
	block := storageVerificationCheck / period
	blockPerDay := secondsPerDay / period
	return block+1 == number%blockPerDay && block != number
}