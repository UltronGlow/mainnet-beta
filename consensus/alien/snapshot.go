// Copyright 2021 The utg Authors
// This file is part of the utg library.
//
// The utg library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The utg library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the utg library. If not, see <http://www.gnu.org/licenses/>.

// Package alien implements the delegated-proof-of-stake consensus engine.

package alien

import (
	"encoding/json"
	"errors"
	"github.com/UltronGlow/UltronGlow-Origin/common"
	"github.com/UltronGlow/UltronGlow-Origin/consensus"
	"github.com/UltronGlow/UltronGlow-Origin/core/state"
	"github.com/UltronGlow/UltronGlow-Origin/core/types"
	"github.com/UltronGlow/UltronGlow-Origin/ethdb"
	"github.com/UltronGlow/UltronGlow-Origin/log"
	"github.com/UltronGlow/UltronGlow-Origin/params"
	"github.com/UltronGlow/UltronGlow-Origin/rlp"
	"github.com/hashicorp/golang-lru"
	"golang.org/x/crypto/sha3"
	"math/big"
	"sort"
	"strings"
	"time"
)

const (
	defaultFullCredit               = 100 // no punished
	missingPublishCredit            = 30  // punished for missing one block seal
	signRewardCredit                = 10  // seal one block
	autoRewardCredit                = 1   // credit auto recover for each block
	minCalSignerQueueCredit         = 30  // when calculate the signerQueue
	defaultOfficialMaxSignerCount   = 21  // official max signer count
	defaultOfficialFirstLevelCount  = 10  // official first level , 100% in signer queue
	defaultOfficialSecondLevelCount = 20  // official second level, 60% in signer queue
	defaultOfficialThirdLevelCount  = 30  // official third level, 40% in signer queue
	defaultOfficialMaxValidCount    = 50  // official max valid candidate count, sort by vote

	maxUncheckBalanceVoteCount = 10000 // not check current balance when calculate expired
	// the credit of one signer is at least minCalSignerQueueCredit
	candidateStateNormal = 1
	candidateMaxLen      = 500 // if candidateNeedPD is false and candidate is more than candidateMaxLen, then minimum tickets candidates will be remove in each LCRS*loop
	// reward for side chain
	scRewardDelayLoopCount     = 0                          //
	scRewardExpiredLoopCount   = scRewardDelayLoopCount + 4 //
	scMaxCountPerPeriod        = 6
	scMaxConfirmedRecordLength = defaultOfficialMaxSignerCount * 50 // max record length for each side chain
	// proposal refund
	proposalRefundDelayLoopCount   = 0
	proposalRefundExpiredLoopCount = proposalRefundDelayLoopCount + 2
	// notice
	mcNoticeClearDelayLoopCount = 4 // this count can be hundreds times
	scNoticeClearDelayLoopCount = mcNoticeClearDelayLoopCount * scMaxCountPerPeriod * 2
	scGasChargingDelayLoopCount = 1 // 1 is always enough
	// bug fix
	bugFixBlockNumber = 14456164 // fix bug for header
)

// Score to calculate at one main chain block, for calculate the side chain reward
type SCBlockReward struct {
	RewardScoreMap map[common.Address]uint64 `json:"rewardscore"` //sum(this value) in one period == 100
}

// Record for one side chain
type SCReward struct {
	SCBlockRewardMap map[uint64]*SCBlockReward `json:"scblockrewards"`
}

type SCRentInfo struct {
	RentPerPeriod   *big.Int `json:"rentPerPeriod"`
	MaxRewardNumber *big.Int `json:"maxRewardNumber"`
}

// SCRecord is the state record for side chain
type SCRecord struct {
	Record              map[uint64][]*SCConfirmation `json:"record"`              // Confirmation Record of one side chain
	LastConfirmedNumber uint64                       `json:"lastConfirmedNumber"` // Last confirmed header number of one side chain
	MaxHeaderNumber     uint64                       `json:"maxHeaderNumber"`     // max header number of one side chain
	CountPerPeriod      uint64                       `json:"countPerPeriod"`      // block sealed per period on this side chain
	RewardPerPeriod     uint64                       `json:"rewardPerPeriod"`     // full reward per period, number per thousand
	RentReward          map[common.Hash]*SCRentInfo  `json:"rentReward"`          // reward info by rent
}

type NoticeCR struct {
	NRecord map[common.Address]bool `json:"noticeConfirmRecord"`
	Number  uint64                  `json:"firstReceivedNumber"` // this number will fill when there are more than 2/3+1 maxSignerCnt
	Type    uint64                  `json:"noticeType"`
	Success bool                    `json:"success"`
}

// CCNotice (cross chain notice) contain the information main chain need to notify given side chain
type CCNotice struct {
	CurrentCharging map[common.Hash]GasCharging `json:"currentCharging"` // common.Hash here is the proposal txHash not the hash of side chain
	ConfirmReceived map[common.Hash]NoticeCR    `json:"confirmReceived"` // record the confirm address
}

type RevenueParameter struct {
	RevenueAddress  common.Address `json:"revenueaddress"`
	RevenueContract common.Address `json:"contractaddress"`
	MultiSignature  common.Address `json:"multisignatureaddress"`
}

type PledgeItem struct {
	Amount *big.Int `json:"lockamount"`
	//Reward          *big.Int       `json:"bandwidthreward"`
	PledgeType      uint32         `json:"type"`
	Playment        *big.Int       `json:"playment"`
	LockPeriod      uint32         `json:"lockperiod"`
	RlsPeriod       uint32         `json:"releaseperiod"`
	Interval        uint32         `json:"releaseinterval"`
	StartHigh       uint64         `json:"startblocknumber"`
	TargetAddress   common.Address `json:"targetaddress"`
	RevenueAddress  common.Address `json:"revenueaddress"`
	RevenueContract common.Address `json:"contractaddress"`
	MultiSignature  common.Address `json:"multisignatureaddress"`

	BurnAddress common.Address `json:"burnaddress"`
	BurnRatio   *big.Int       `json:"burnratio"`
	BurnAmount  *big.Int       `json:"burnamount"`
}

type ClaimedBandwidth struct {
	ISPQosID         uint32 `json:"ispqosid"`
	BandwidthClaimed uint32 `json:"bandwidthclaimed"`
}

type LockParameter struct {
	LockPeriod uint32 `json:"LockPeriod"`
	RlsPeriod  uint32 `json:"ReleasePeriod"`
	Interval   uint32 `json:"ReleaseInterval"`
}

type CandidateState struct {
	SignerNumber uint64   `json:"signernumber"`
	Stake        *big.Int `json:"stake"`
}

type SystemParameter struct {
	ExchRate       uint32                    `json:"ExchangeRatio"`
	OffLine        uint32                    `json:"OfflinePenalty"`
	Deposit        map[uint32]*big.Int       `json:"SeniorityThreshold"`
	QosConfig      map[uint32]uint32         `json:"BandwidthQOS"`
	ManagerAddress map[uint32]common.Address `json:"FoundationAddress"`
	LockParameters map[uint32]*LockParameter `json:"PledgeParameter"`
}

type FlowMinerReport struct {
	Target       common.Address `json:"target"`
	Hash         common.Hash    `json:"hash"`
	ReportNumber uint32         `json:"reportnumber"`
	FlowValue1   uint64         `json:"rewardflow"`
	FlowValue2   uint64         `json:"consumeflow"`
}

type FULLBalanceData struct {
	Balance   *big.Int                 `json:"purchasetotal"`
	CostTotal map[common.Hash]*big.Int `json:"consumetotal"`
}

type LockBalanceData struct {
	RewardBalance   map[uint32]*big.Int                        `json:"rewardbalance"`
	LockBalance     map[uint64]map[uint32]*PledgeItem          `json:"lockbalance"`
	RewardBalanceV1 map[uint32]map[common.Address]*LockTmpData `json:"rewardbalanceV1"`
	LockBalanceV1   map[uint64]map[uint32]map[common.Address]*PledgeItem         `json:"lockbalanceV1"`
}
type LockTmpData struct {
	Amount         *big.Int
	RevenueAddress common.Address
}
type PosPledgeItem struct {
	Manager     common.Address                `json:"manager"`
	Active      uint64                        `json:"active"`
	TotalAmount *big.Int                      `json:"totalamount"`
	Detail      map[common.Hash]*PledgeDetail `json:"detail"`
	LastPunish  uint64                        `json:"lastpunish"`
	DisRate     *big.Int                      `json:"distributerate"`
}

type PledgeDetail struct {
	Address common.Address `json:"address"`
	Height  uint64         `json:"height"`
	Amount  *big.Int       `json:"amount"`
}

// Snapshot is the state of the authorization voting at a given point in time.
type Snapshot struct {
	config   *params.AlienConfig // Consensus engine parameters to fine tune behavior
	sigcache *lru.ARCCache       // Cache of recent block signatures to speed up ecrecover
	LCRS     uint64              // Loop count to recreate signers from top tally

	Period          uint64                                            `json:"period"`            // Period of seal each block
	Number          uint64                                            `json:"number"`            // Block number where the snapshot was created
	ConfirmedNumber uint64                                            `json:"confirmedNumber"`   // Block number confirmed when the snapshot was created
	Hash            common.Hash                                       `json:"hash"`              // Block hash where the snapshot was created
	HistoryHash     []common.Hash                                     `json:"historyHash"`       // Block hash list for two recent loop
	Signers         []*common.Address                                 `json:"signers"`           // Signers queue in current header
	Votes           map[common.Address]*Vote                          `json:"votes"`             // All validate votes from genesis block
	Tally           map[common.Address]*big.Int                       `json:"tally"`             // Stake for each candidate address
	Voters          map[common.Address]*big.Int                       `json:"voters"`            // Block number for each voter address
	Candidates      map[common.Address]uint64                         `json:"candidates"`        // Candidates for Signers (0- adding procedure 1- normal 2- removing procedure)
	Punished        map[common.Address]uint64                         `json:"punished"`          // The signer be punished count cause of missing seal
	Confirmations   map[uint64][]*common.Address                      `json:"confirms"`          // The signer confirm given block number
	Proposals       map[common.Hash]*Proposal                         `json:"proposals"`         // The Proposals going or success (failed proposal will be removed)
	HeaderTime      uint64                                            `json:"headerTime"`        // Time of the current header
	LoopStartTime   uint64                                            `json:"loopStartTime"`     // Start Time of the current loop
	ProposalRefund  map[uint64]map[common.Address]*big.Int            `json:"proposalRefund"`    // Refund proposal deposit
	SCCoinbase      map[common.Hash]map[common.Address]common.Address `json:"sideChainCoinbase"` // main chain set Coinbase of side chain setting
	SCRecordMap     map[common.Hash]*SCRecord                         `json:"sideChainRecord"`   // main chain record Confirmation of side chain setting
	SCRewardMap     map[common.Hash]*SCReward                         `json:"sideChainReward"`   // main chain record Side Chain Reward
	SCNoticeMap     map[common.Hash]*CCNotice                         `json:"sideChainNotice"`   // main chain record Notification to side chain
	LocalNotice     *CCNotice                                         `json:"localNotice"`       // side chain record Notification
	MinerReward     uint64                                            `json:"minerReward"`       // miner reward per thousand
	MinVB           *big.Int                                          `json:"minVoterBalance"`   // min voter balance
	FULBalance      map[common.Address]*FULLBalanceData               `json:"fulbalancedata"`
	RevenueNormal   map[common.Address]*RevenueParameter              `json:"normalrevenueaddress"`
	RevenueFlow     map[common.Address]*RevenueParameter              `json:"flowrevenueaddress"`
	CandidatePledge map[common.Address]*PledgeItem                    `json:"candidatepledge"`
	TallyMiner      map[common.Address]*CandidateState                `json:"tallyminer"` // Stake for each miner address
	FlowPledge      map[common.Address]*PledgeItem                    `json:"flowminerpledge"`
	Bandwidth       map[common.Address]*ClaimedBandwidth              `json:"claimedbandwidth"`
	FlowHarvest     *big.Int                                          `json:"flowharvest"`
	//FlowRevenue     map[common.Address]*LockBalanceData                 `json:"flowrevenve"`
	FlowRevenue  *LockProfitSnap
	SystemConfig SystemParameter `json:"systemconfig"`
	FlowMiner    *FlowMinerSnap  `json:"flowminer"`
	//FlowMiner      map[common.Address]map[common.Hash]*FlowMinerReport `json:"flowminerCurr"`
	//FlowMinerPrev  map[common.Address]map[common.Hash]*FlowMinerReport `json:"flowminerPrev"`
	FlowTotal      *big.Int                          `json:"flowtotal"`
	SCMinerRevenue map[common.Address]common.Address `json:"scminerrevenue"`
	SCFlowPledge   map[common.Address]bool           `json:"scflowpledge"`
	SCFULBalance   map[common.Address]*big.Int       `json:"fulbalance"`
	SignerMissing  []common.Address                  `json:"signermissing"`
	TallySigner    map[common.Address]uint64         `json:"tallySigner"`
	//StoragePledge      map[common.Address]*PledgeItem                    `json:"Storagepledge"`
	StorageData *StorageData `json:"storagedata"`

	ExStateRoot        common.Hash                          `json:"extrasstateRoot"`
	GrantListRoot      common.Hash                          `json:"grantListRoot"`
	RevenueStorage     map[common.Address]*RevenueParameter `json:"storagerevenueaddress"`
	SRT                SRTState                             `json:"-"`
	SRTHash            common.Hash                          `json:"srthash"`
	STGBandwidthMakeup map[common.Address]*BandwidthMakeup  `json:"stgbandwidthmakeup"`
	PosPledge          map[common.Address]*PosPledgeItem    `json:"pospledge"`
	TotalLeaseSpace    *big.Int                             `json:"totalleasespace"`
	SpData             *SpData                              `json:"SpoolData"`
}

var (
	errIncorrectTallyCount = errors.New("incorrect tally count")
	errAllStakeMissing     = errors.New("all stake for this signer is zero")
)

// SCCurrentBlockReward is base on scMaxCountPerPeriod = 6
var SCCurrentBlockReward = map[uint64]map[uint64]uint64{
	1: {1: 100},
	2: {1: 30, 2: 70},
	3: {1: 15, 2: 30, 3: 55},
	4: {1: 5, 2: 15, 3: 30, 4: 50},
	5: {1: 5, 2: 10, 3: 15, 4: 25, 5: 45},
	6: {1: 1, 2: 4, 3: 10, 4: 15, 5: 25, 6: 45},
}

// newSnapshot creates a new snapshot with the specified startup parameters. only ever use if for
// the genesis block.
func newSnapshot(config *params.AlienConfig, sigcache *lru.ARCCache, hash common.Hash, votes []*Vote, lcrs uint64) *Snapshot {

	snap := &Snapshot{
		config:          config,
		sigcache:        sigcache,
		LCRS:            lcrs,
		Period:          config.Period,
		Number:          0,
		ConfirmedNumber: 0,
		Hash:            hash,
		HistoryHash:     []common.Hash{},
		Signers:         []*common.Address{},
		Votes:           make(map[common.Address]*Vote),
		Tally:           make(map[common.Address]*big.Int),
		Voters:          make(map[common.Address]*big.Int),
		Punished:        make(map[common.Address]uint64),
		Candidates:      make(map[common.Address]uint64),
		Confirmations:   make(map[uint64][]*common.Address),
		Proposals:       make(map[common.Hash]*Proposal),
		HeaderTime:      uint64(time.Now().Unix()) - 1,
		LoopStartTime:   config.GenesisTimestamp,
		SCCoinbase:      make(map[common.Hash]map[common.Address]common.Address),
		SCRecordMap:     make(map[common.Hash]*SCRecord),
		SCRewardMap:     make(map[common.Hash]*SCReward),
		SCNoticeMap:     make(map[common.Hash]*CCNotice),
		LocalNotice:     &CCNotice{CurrentCharging: make(map[common.Hash]GasCharging), ConfirmReceived: make(map[common.Hash]NoticeCR)},
		ProposalRefund:  make(map[uint64]map[common.Address]*big.Int),
		MinerReward:     minerRewardPerThousand,
		MinVB:           config.MinVoterBalance,
		FULBalance:      make(map[common.Address]*FULLBalanceData),
		RevenueNormal:   make(map[common.Address]*RevenueParameter),
		RevenueFlow:     make(map[common.Address]*RevenueParameter),
		CandidatePledge: make(map[common.Address]*PledgeItem),
		TallyMiner:      make(map[common.Address]*CandidateState),
		FlowPledge:      make(map[common.Address]*PledgeItem),
		Bandwidth:       make(map[common.Address]*ClaimedBandwidth),
		FlowHarvest:     big.NewInt(0),
		FlowRevenue:     NewLockProfitSnap(),
		StorageData:     NewStorageSnap(),
		SystemConfig: SystemParameter{
			ExchRate:       10000,
			OffLine:        10000,
			Deposit:        make(map[uint32]*big.Int),
			QosConfig:      make(map[uint32]uint32),
			ManagerAddress: make(map[uint32]common.Address),
			LockParameters: make(map[uint32]*LockParameter),
		},
		FlowMiner:      NewFlowMinerSnap(config.GenesisTimestamp),
		FlowTotal:      big.NewInt(0),
		SCMinerRevenue: make(map[common.Address]common.Address),
		SCFlowPledge:   make(map[common.Address]bool),
		SCFULBalance:   make(map[common.Address]*big.Int),
		SignerMissing:  []common.Address{},
		TallySigner:    make(map[common.Address]uint64),
		RevenueStorage: make(map[common.Address]*RevenueParameter),
		SpData:         NewSPSnap(),
	}
	snap.HistoryHash = append(snap.HistoryHash, hash)

	for _, vote := range votes {
		// init Votes from each vote
		snap.Votes[vote.Voter] = vote
		// init Tally
		_, ok := snap.Tally[vote.Candidate]
		if !ok {
			snap.Tally[vote.Candidate] = big.NewInt(0)
		}
		snap.Tally[vote.Candidate].Add(snap.Tally[vote.Candidate], vote.Stake)
		// init Voters
		snap.Voters[vote.Voter] = big.NewInt(0) // block number is 0 , vote in genesis block
		// init Candidates
		snap.Candidates[vote.Voter] = candidateStateNormal
	}

	if len(config.SelfVoteSigners) > 0 {
		var prefixSelfVoteSigners []common.Address
		for _, unPrefixSelfVoteSigners := range config.SelfVoteSigners {
			prefixSelfVoteSigners = append(prefixSelfVoteSigners, common.Address(unPrefixSelfVoteSigners))
		}
		for i := 0; i < int(config.MaxSignerCount); i++ {
			snap.Signers = append(snap.Signers, &prefixSelfVoteSigners[i%len(prefixSelfVoteSigners)])
		}
	}

	snap.SystemConfig.LockParameters[sscEnumCndLock] = &LockParameter{
		LockPeriod: uint32(signerPledgeLockParamPeriod / config.Period),
		RlsPeriod:  signerPledgeLockParamRlsPeriod,
		Interval:   signerPledgeLockParamInterval,
	}
	snap.SystemConfig.LockParameters[sscEnumFlwLock] = &LockParameter{
		LockPeriod: uint32(flowPledgeLockParamPeriod / config.Period),
		RlsPeriod:  flowPledgeLockParamRlsPeriod,
		Interval:   flowPledgeLockParamInterval,
	}
	snap.SystemConfig.LockParameters[sscEnumRwdLock] = &LockParameter{
		//LockPeriod: uint32(30 * 24 * 60 * 60 / config.Period),
		LockPeriod: uint32(rewardLockParamPeriod / config.Period),
		RlsPeriod:  uint32(rewardLockParamRlsPeriod / config.Period),
		Interval:   uint32(rewardLockParamInterval / config.Period),
	}
	snap.SystemConfig.Deposit[0] = new(big.Int).Set(minCndPledgeBalance)
	snap.SystemConfig.Deposit[sscEnumSignerReward] = new(big.Int).Set(minSignerLockBalance)
	snap.SystemConfig.Deposit[sscEnumFlwReward] = new(big.Int).Set(minFlwLockBalance)
	snap.SystemConfig.Deposit[sscEnumBandwidthReward] = new(big.Int).Set(minBandwidthLockBalance)
	snap.SystemConfig.ManagerAddress[sscEnumExchRate] = managerAddressExchRate
	snap.SystemConfig.ManagerAddress[sscEnumSystem] = managerAddressSystem
	snap.SystemConfig.ManagerAddress[sscEnumWdthPnsh] = managerAddressWdthPnsh
	snap.SystemConfig.ManagerAddress[sscEnumFlowReport] = managerAddressFlowReport
	snap.SystemConfig.Deposit[sscEnumPStoragePledgeID] = new(big.Int).Set(storagePledgeIndex)
	snap.SystemConfig.Deposit[sscEnumLeaseExpires] = new(big.Int).Set(defaultLeaseExpires)
	snap.SystemConfig.Deposit[sscEnumMinimumRent] = new(big.Int).Set(minimumRentDay)
	snap.SystemConfig.Deposit[sscEnumMaximumRent] = new(big.Int).Set(maximumRentDay)
	snap.SystemConfig.Deposit[sscEnumPosCommitPeriod] = new(big.Int).Set(posCommitPeriod)
	snap.SystemConfig.Deposit[sscEnumPosBeyondCommitPeriod] = new(big.Int).Set(posBeyondCommitPeriod)
	snap.SystemConfig.Deposit[sscEnumPosWithinCommitPeriod] = new(big.Int).Set(posWithinCommitPeriod)
	return snap
}

// loadSnapshot loads an existing snapshot from the database.
func loadSnapshot(config *params.AlienConfig, sigcache *lru.ARCCache, db ethdb.Database, hash common.Hash) (*Snapshot, error) {
	blob, err := db.Get(append([]byte("alien-"), hash[:]...))
	if err != nil {
		return nil, err
	}
	snap := new(Snapshot)
	if err := json.Unmarshal(blob, snap); err != nil {
		return nil, err
	}
	snap.config = config
	snap.sigcache = sigcache

	// miner reward per thousand proposal must larger than 0
	// so minerReward is zeron only when update the program
	if snap.MinerReward == 0 {
		snap.MinerReward = minerRewardPerThousand
	}
	if snap.MinVB == nil {
		snap.MinVB = new(big.Int).Set(minVoterBalance)
	}

	if 0 == snap.SystemConfig.ExchRate {
		snap.SystemConfig.ExchRate = 10000
	}
	if 0 == snap.SystemConfig.OffLine {
		snap.SystemConfig.OffLine = 10000
	}
	if _, ok := snap.SystemConfig.Deposit[0]; !ok || 0 > snap.SystemConfig.Deposit[0].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[0] = new(big.Int).Set(minCndPledgeBalance)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumSignerReward]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumSignerReward].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumSignerReward] = new(big.Int).Set(minSignerLockBalance)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumFlwReward]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumFlwReward].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumFlwReward] = new(big.Int).Set(minFlwLockBalance)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumBandwidthReward]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumBandwidthReward].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumBandwidthReward] = new(big.Int).Set(minBandwidthLockBalance)
	}
	if _, ok := snap.SystemConfig.LockParameters[sscEnumCndLock]; !ok {
		snap.SystemConfig.LockParameters[sscEnumCndLock] = &LockParameter{
			LockPeriod: uint32(signerPledgeLockParamPeriod / config.Period),
			RlsPeriod:  signerPledgeLockParamRlsPeriod,
			Interval:   signerPledgeLockParamInterval,
		}
	}
	if _, ok := snap.SystemConfig.LockParameters[sscEnumFlwLock]; !ok {
		snap.SystemConfig.LockParameters[sscEnumFlwLock] = &LockParameter{
			LockPeriod: uint32(flowPledgeLockParamPeriod / config.Period),
			RlsPeriod:  flowPledgeLockParamRlsPeriod,
			Interval:   flowPledgeLockParamInterval,
		}
	}
	if _, ok := snap.SystemConfig.LockParameters[sscEnumRwdLock]; !ok {
		snap.SystemConfig.LockParameters[sscEnumRwdLock] = &LockParameter{
			LockPeriod: uint32(rewardLockParamPeriod / config.Period),
			RlsPeriod:  uint32(rewardLockParamRlsPeriod / config.Period),
			Interval:   uint32(rewardLockParamInterval / config.Period),
		}
	}
	if _, ok := snap.SystemConfig.ManagerAddress[sscEnumExchRate]; !ok {
		snap.SystemConfig.ManagerAddress[sscEnumExchRate] = managerAddressExchRate
	}
	if _, ok := snap.SystemConfig.ManagerAddress[sscEnumSystem]; !ok {
		snap.SystemConfig.ManagerAddress[sscEnumSystem] = managerAddressSystem
	}
	if _, ok := snap.SystemConfig.ManagerAddress[sscEnumWdthPnsh]; !ok {
		snap.SystemConfig.ManagerAddress[sscEnumWdthPnsh] = managerAddressWdthPnsh
	}
	if _, ok := snap.SystemConfig.ManagerAddress[sscEnumFlowReport]; !ok {
		snap.SystemConfig.ManagerAddress[sscEnumFlowReport] = managerAddressFlowReport
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumStoragePrice]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumStoragePrice].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumStoragePrice] = new(big.Int).Set(baseStoragePrice)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumPStoragePledgeID]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumPStoragePledgeID].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumPStoragePledgeID] = new(big.Int).Set(storagePledgeIndex)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumLeaseExpires]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumLeaseExpires].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumLeaseExpires] = new(big.Int).Set(defaultLeaseExpires)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumMinimumRent]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumMinimumRent].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumMinimumRent] = new(big.Int).Set(minimumRentDay)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumMaximumRent]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumMaximumRent].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumMaximumRent] = new(big.Int).Set(maximumRentDay)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumPosCommitPeriod]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumPosCommitPeriod].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumPosCommitPeriod] = new(big.Int).Set(posCommitPeriod)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumPosBeyondCommitPeriod]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumPosBeyondCommitPeriod].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumPosBeyondCommitPeriod] = new(big.Int).Set(posBeyondCommitPeriod)
	}
	if _, ok := snap.SystemConfig.Deposit[sscEnumPosWithinCommitPeriod]; !ok || 0 > snap.SystemConfig.Deposit[sscEnumPosWithinCommitPeriod].Cmp(big.NewInt(0)) {
		snap.SystemConfig.Deposit[sscEnumPosWithinCommitPeriod] = new(big.Int).Set(posWithinCommitPeriod)
	}
	nilHash := common.Hash{}
	if snap.SRTHash != nilHash {
		snap.SRT, err = NewSRT(snap.SRTHash, db)
		if err != nil {
			return nil, err
		}
		err = snap.SRT.Load(db, snap.SRTHash)
		if err != nil {
			return nil, err
		}
	}

	return snap, nil
}

// store inserts the snapshot into the database.
func (s *Snapshot) store(db ethdb.Database) error {
	err := s.FlowRevenue.saveCacheL1(db)
	if err != nil {
		return err
	}
	err = s.FlowMiner.store(db, s.Number)
	if err != nil {
		return err
	}
	if s.SRT != nil {
		s.SRTHash, err = s.SRT.Save(db)
		if err != nil {
			return err
		}
		//s.SRTHash=s.SRT.Root()
	}

	blob, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return db.Put(append([]byte("alien-"), s.Hash[:]...), blob)
}

// copy creates a deep copy of the snapshot, though not the individual votes.
func (s *Snapshot) copy() *Snapshot {
	cpy := &Snapshot{
		config:          s.config,
		sigcache:        s.sigcache,
		LCRS:            s.LCRS,
		Period:          s.Period,
		Number:          s.Number,
		ConfirmedNumber: s.ConfirmedNumber,
		Hash:            s.Hash,
		HistoryHash:     make([]common.Hash, len(s.HistoryHash)),

		Signers:       make([]*common.Address, len(s.Signers)),
		Votes:         make(map[common.Address]*Vote),
		Tally:         make(map[common.Address]*big.Int),
		Voters:        make(map[common.Address]*big.Int),
		Candidates:    make(map[common.Address]uint64),
		Punished:      make(map[common.Address]uint64),
		Proposals:     make(map[common.Hash]*Proposal),
		Confirmations: make(map[uint64][]*common.Address),

		HeaderTime:     s.HeaderTime,
		LoopStartTime:  s.LoopStartTime,
		SCCoinbase:     make(map[common.Hash]map[common.Address]common.Address),
		SCRecordMap:    make(map[common.Hash]*SCRecord),
		SCRewardMap:    make(map[common.Hash]*SCReward),
		SCNoticeMap:    make(map[common.Hash]*CCNotice),
		LocalNotice:    &CCNotice{CurrentCharging: make(map[common.Hash]GasCharging), ConfirmReceived: make(map[common.Hash]NoticeCR)},
		ProposalRefund: make(map[uint64]map[common.Address]*big.Int),

		MinerReward:     s.MinerReward,
		MinVB:           nil,
		FULBalance:      make(map[common.Address]*FULLBalanceData),
		RevenueNormal:   make(map[common.Address]*RevenueParameter),
		RevenueFlow:     make(map[common.Address]*RevenueParameter),
		CandidatePledge: make(map[common.Address]*PledgeItem),
		TallyMiner:      make(map[common.Address]*CandidateState),
		FlowPledge:      make(map[common.Address]*PledgeItem),
		Bandwidth:       make(map[common.Address]*ClaimedBandwidth),
		FlowHarvest:     s.FlowHarvest,
		FlowRevenue:     s.FlowRevenue.copy(),
		SystemConfig: SystemParameter{
			ExchRate:       s.SystemConfig.ExchRate,
			OffLine:        s.SystemConfig.OffLine,
			Deposit:        make(map[uint32]*big.Int),
			QosConfig:      make(map[uint32]uint32),
			ManagerAddress: make(map[uint32]common.Address),
			LockParameters: make(map[uint32]*LockParameter),
		},
		FlowMiner:      s.FlowMiner.copy(),
		FlowTotal:      new(big.Int).Set(s.FlowTotal),
		SCMinerRevenue: make(map[common.Address]common.Address),
		SCFlowPledge:   make(map[common.Address]bool),
		SCFULBalance:   make(map[common.Address]*big.Int),
		SignerMissing:  make([]common.Address, len(s.SignerMissing)),
		TallySigner:    make(map[common.Address]uint64),
		ExStateRoot:    s.ExStateRoot,
		GrantListRoot:  s.GrantListRoot,
		RevenueStorage: make(map[common.Address]*RevenueParameter),
		SRT:            nil,
		SRTHash:        s.SRTHash,
	}
	if s.StorageData != nil {
		cpy.StorageData = s.StorageData.copy()
	}
	if s.SpData != nil {
		cpy.SpData = s.SpData.copy()
	}
	if s.SRT != nil {
		cpy.SRT = s.SRT.Copy()
		cpy.SRTHash = cpy.SRT.Root()
	}
	if s.STGBandwidthMakeup != nil {
		cpy.STGBandwidthMakeup = make(map[common.Address]*BandwidthMakeup, 0)
		for paddr, bw := range s.STGBandwidthMakeup {
			cpy.STGBandwidthMakeup[paddr] = &BandwidthMakeup{
				OldBandwidth:  new(big.Int).Set(bw.OldBandwidth),
				BurnRatio:     new(big.Int).Set(bw.BurnRatio),
				DepositMakeup: new(big.Int).Set(bw.DepositMakeup),
				AdjustCount:   bw.AdjustCount,
			}
		}
	}
	if s.PosPledge != nil {
		cpy.PosPledge = make(map[common.Address]*PosPledgeItem, 0)
		for addr, item := range s.PosPledge {
			cpy.PosPledge[addr] = &PosPledgeItem{
				Manager:     item.Manager,
				Active:      item.Active,
				TotalAmount: new(big.Int).Set(item.TotalAmount),
				Detail:      make(map[common.Hash]*PledgeDetail, 0),
				LastPunish:  item.LastPunish,
				DisRate:     new(big.Int).Set(item.DisRate),
			}
			for hash, detail := range item.Detail {
				cpy.PosPledge[addr].Detail[hash] = &PledgeDetail{
					Address: detail.Address,
					Height:  detail.Height,
					Amount:  new(big.Int).Set(detail.Amount),
				}
			}
		}
	}
	if s.TotalLeaseSpace != nil {
		cpy.TotalLeaseSpace = new(big.Int).Set(s.TotalLeaseSpace)
	}
	copy(cpy.HistoryHash, s.HistoryHash)
	copy(cpy.Signers, s.Signers)
	copy(cpy.SignerMissing, s.SignerMissing)
	for voter, vote := range s.Votes {
		cpy.Votes[voter] = &Vote{
			Voter:     vote.Voter,
			Candidate: vote.Candidate,
			Stake:     new(big.Int).Set(vote.Stake),
		}
	}
	for candidate, signNumber := range s.TallySigner {
		cpy.TallySigner[candidate] = signNumber
	}
	for candidate, tally := range s.Tally {
		cpy.Tally[candidate] = new(big.Int).Set(tally)
	}
	for voter, number := range s.Voters {
		cpy.Voters[voter] = new(big.Int).Set(number)
	}
	for candidate, state := range s.Candidates {
		cpy.Candidates[candidate] = state
	}
	for signer, cnt := range s.Punished {
		cpy.Punished[signer] = cnt
	}
	for blockNumber, confirmers := range s.Confirmations {
		cpy.Confirmations[blockNumber] = make([]*common.Address, len(confirmers))
		copy(cpy.Confirmations[blockNumber], confirmers)
	}
	for txHash, proposal := range s.Proposals {
		cpy.Proposals[txHash] = proposal.copy()
	}
	for hash, sc := range s.SCCoinbase {
		cpy.SCCoinbase[hash] = make(map[common.Address]common.Address)
		for addr, signer := range sc {
			cpy.SCCoinbase[hash][addr] = signer
		}
	}
	for hash, scc := range s.SCRecordMap {
		cpy.SCRecordMap[hash] = &SCRecord{
			LastConfirmedNumber: scc.LastConfirmedNumber,
			MaxHeaderNumber:     scc.MaxHeaderNumber,
			CountPerPeriod:      scc.CountPerPeriod,
			RewardPerPeriod:     scc.RewardPerPeriod,
			Record:              make(map[uint64][]*SCConfirmation),
			RentReward:          make(map[common.Hash]*SCRentInfo),
		}
		for number, scConfirmation := range scc.Record {
			cpy.SCRecordMap[hash].Record[number] = make([]*SCConfirmation, len(scConfirmation))
			copy(cpy.SCRecordMap[hash].Record[number], scConfirmation)
		}
		for rentHash, scRentInfo := range scc.RentReward {
			cpy.SCRecordMap[hash].RentReward[rentHash] = &SCRentInfo{new(big.Int).Set(scRentInfo.RentPerPeriod), new(big.Int).Set(scRentInfo.MaxRewardNumber)}
		}
		for rentHash, scRentInfo := range scc.RentReward {
			cpy.SCRecordMap[hash].RentReward[rentHash] = &SCRentInfo{new(big.Int).Set(scRentInfo.RentPerPeriod), new(big.Int).Set(scRentInfo.MaxRewardNumber)}
		}
	}

	for hash, sca := range s.SCRewardMap {
		cpy.SCRewardMap[hash] = &SCReward{
			SCBlockRewardMap: make(map[uint64]*SCBlockReward),
		}
		for number, blockReward := range sca.SCBlockRewardMap {
			cpy.SCRewardMap[hash].SCBlockRewardMap[number] = &SCBlockReward{
				RewardScoreMap: make(map[common.Address]uint64),
			}
			for addr, score := range blockReward.RewardScoreMap {
				cpy.SCRewardMap[hash].SCBlockRewardMap[number].RewardScoreMap[addr] = score
			}
		}
	}

	for hash, scn := range s.SCNoticeMap {
		cpy.SCNoticeMap[hash] = &CCNotice{
			CurrentCharging: make(map[common.Hash]GasCharging),
			ConfirmReceived: make(map[common.Hash]NoticeCR),
		}
		for txHash, charge := range scn.CurrentCharging {
			cpy.SCNoticeMap[hash].CurrentCharging[txHash] = GasCharging{charge.Target, charge.Volume, charge.Hash}
		}
		for txHash, confirm := range scn.ConfirmReceived {
			cpy.SCNoticeMap[hash].ConfirmReceived[txHash] = NoticeCR{make(map[common.Address]bool), confirm.Number, confirm.Type, confirm.Success}
			for addr, b := range confirm.NRecord {
				cpy.SCNoticeMap[hash].ConfirmReceived[txHash].NRecord[addr] = b
			}
		}
	}

	for txHash, charge := range s.LocalNotice.CurrentCharging {
		cpy.LocalNotice.CurrentCharging[txHash] = GasCharging{charge.Target, charge.Volume, charge.Hash}
	}
	for txHash, confirm := range s.LocalNotice.ConfirmReceived {
		cpy.LocalNotice.ConfirmReceived[txHash] = NoticeCR{make(map[common.Address]bool), confirm.Number, confirm.Type, confirm.Success}
		for addr, b := range confirm.NRecord {
			cpy.LocalNotice.ConfirmReceived[txHash].NRecord[addr] = b
		}
	}

	for number, refund := range s.ProposalRefund {
		cpy.ProposalRefund[number] = make(map[common.Address]*big.Int)
		for proposer, deposit := range refund {
			cpy.ProposalRefund[number][proposer] = new(big.Int).Set(deposit)
		}
	}
	// miner reward per thousand proposal must larger than 0
	// so minerReward is zeron only when update the program
	if s.MinerReward == 0 {
		cpy.MinerReward = minerRewardPerThousand
	}
	if s.MinVB == nil {
		cpy.MinVB = new(big.Int).Set(minVoterBalance)
	} else {
		cpy.MinVB = new(big.Int).Set(s.MinVB)
	}

	for who, balance := range s.FULBalance {
		cpy.FULBalance[who] = &FULLBalanceData{
			Balance:   new(big.Int).Set(balance.Balance),
			CostTotal: make(map[common.Hash]*big.Int),
		}
		for sc, total := range balance.CostTotal {
			cpy.FULBalance[who].CostTotal[sc] = new(big.Int).Set(total)
		}
	}
	for who, revenue := range s.RevenueNormal {
		cpy.RevenueNormal[who] = &RevenueParameter{
			RevenueAddress:  revenue.RevenueAddress,
			RevenueContract: revenue.RevenueContract,
			MultiSignature:  revenue.MultiSignature,
		}
	}
	for who, revenue := range s.RevenueFlow {
		cpy.RevenueFlow[who] = &RevenueParameter{
			RevenueAddress:  revenue.RevenueAddress,
			RevenueContract: revenue.RevenueContract,
			MultiSignature:  revenue.MultiSignature,
		}
	}
	for who, pledge := range s.CandidatePledge {
		cpy.CandidatePledge[who] = pledge.copy()
	}
	for who, status := range s.TallyMiner {
		cpy.TallyMiner[who] = &CandidateState{
			SignerNumber: status.SignerNumber,
			Stake:        new(big.Int).Set(status.Stake),
		}
	}
	for who, pledge := range s.FlowPledge {
		cpy.FlowPledge[who] = pledge.copy()
	}
	for who, bandwidth := range s.Bandwidth {
		cpy.Bandwidth[who] = &ClaimedBandwidth{
			ISPQosID:         bandwidth.ISPQosID,
			BandwidthClaimed: bandwidth.BandwidthClaimed,
		}
	}
	for who, qos := range s.SystemConfig.QosConfig {
		cpy.SystemConfig.QosConfig[who] = qos
	}
	for who, lock := range s.SystemConfig.LockParameters {
		cpy.SystemConfig.LockParameters[who] = &LockParameter{
			LockPeriod: lock.LockPeriod,
			RlsPeriod:  lock.RlsPeriod,
			Interval:   lock.Interval,
		}
	}
	for who, address := range s.SystemConfig.ManagerAddress {
		cpy.SystemConfig.ManagerAddress[who] = address
	}
	if 0 == cpy.SystemConfig.ExchRate {
		cpy.SystemConfig.ExchRate = 10000
	}
	if 0 == cpy.SystemConfig.OffLine {
		cpy.SystemConfig.OffLine = 10000
	}
	for who, value := range s.SystemConfig.Deposit {
		cpy.SystemConfig.Deposit[who] = new(big.Int).Set(value)
	}
	if _, ok := cpy.SystemConfig.Deposit[0]; !ok || 0 > cpy.SystemConfig.Deposit[0].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[0] = new(big.Int).Set(minCndPledgeBalance)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumSignerReward]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumSignerReward].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumSignerReward] = new(big.Int).Set(minSignerLockBalance)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumFlwReward]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumFlwReward].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumFlwReward] = new(big.Int).Set(minFlwLockBalance)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumBandwidthReward]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumBandwidthReward].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumBandwidthReward] = new(big.Int).Set(minBandwidthLockBalance)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumStoragePrice]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumStoragePrice].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumStoragePrice] = new(big.Int).Set(baseStoragePrice)
	}

	if _, ok := cpy.SystemConfig.Deposit[sscEnumPStoragePledgeID]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumPStoragePledgeID].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumPStoragePledgeID] = new(big.Int).Set(storagePledgeIndex)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumLeaseExpires]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumLeaseExpires].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumLeaseExpires] = new(big.Int).Set(defaultLeaseExpires)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumMinimumRent]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumMinimumRent].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumMinimumRent] = new(big.Int).Set(minimumRentDay)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumMaximumRent]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumMaximumRent].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumMaximumRent] = new(big.Int).Set(maximumRentDay)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumPosCommitPeriod]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumPosCommitPeriod].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumPosCommitPeriod] = new(big.Int).Set(posCommitPeriod)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumPosBeyondCommitPeriod]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumPosBeyondCommitPeriod].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumPosBeyondCommitPeriod] = new(big.Int).Set(posBeyondCommitPeriod)
	}
	if _, ok := cpy.SystemConfig.Deposit[sscEnumPosWithinCommitPeriod]; !ok || 0 > cpy.SystemConfig.Deposit[sscEnumPosWithinCommitPeriod].Cmp(big.NewInt(0)) {
		cpy.SystemConfig.Deposit[sscEnumPosWithinCommitPeriod] = new(big.Int).Set(posWithinCommitPeriod)
	}
	if _, ok := cpy.SystemConfig.LockParameters[sscEnumCndLock]; !ok {
		cpy.SystemConfig.LockParameters[sscEnumCndLock] = &LockParameter{
			LockPeriod: uint32(180 * 24 * 60 * 60 / cpy.Period),
			RlsPeriod:  0,
			Interval:   0,
		}
	}
	if _, ok := cpy.SystemConfig.LockParameters[sscEnumFlwLock]; !ok {
		cpy.SystemConfig.LockParameters[sscEnumFlwLock] = &LockParameter{
			LockPeriod: uint32(180 * 24 * 60 * 60 / cpy.Period),
			RlsPeriod:  0,
			Interval:   0,
		}
	}
	if _, ok := cpy.SystemConfig.LockParameters[sscEnumRwdLock]; !ok {
		cpy.SystemConfig.LockParameters[sscEnumRwdLock] = &LockParameter{
			LockPeriod: uint32(30 * 24 * 60 * 60 / cpy.Period),
			RlsPeriod:  uint32(180 * 24 * 60 * 60 / cpy.Period),
			Interval:   uint32(24 * 60 * 60 / cpy.Period),
		}
	}
	if _, ok := cpy.SystemConfig.ManagerAddress[sscEnumExchRate]; !ok {
		cpy.SystemConfig.ManagerAddress[sscEnumExchRate] = managerAddressExchRate
	}
	if _, ok := cpy.SystemConfig.ManagerAddress[sscEnumSystem]; !ok {
		cpy.SystemConfig.ManagerAddress[sscEnumSystem] = managerAddressSystem
	}
	if _, ok := cpy.SystemConfig.ManagerAddress[sscEnumWdthPnsh]; !ok {
		cpy.SystemConfig.ManagerAddress[sscEnumWdthPnsh] = managerAddressWdthPnsh
	}
	if _, ok := cpy.SystemConfig.ManagerAddress[sscEnumFlowReport]; !ok {
		cpy.SystemConfig.ManagerAddress[sscEnumFlowReport] = managerAddressFlowReport
	}
	for who, revenue := range s.RevenueStorage {
		cpy.RevenueStorage[who] = &RevenueParameter{
			RevenueAddress:  revenue.RevenueAddress,
			RevenueContract: revenue.RevenueContract,
			MultiSignature:  revenue.MultiSignature,
		}
	}
	return cpy
}

// apply creates a new authorization snapshot by applying the given headers to
// the original one.
func (s *Snapshot) apply(headers []*types.Header, db ethdb.Database, chain consensus.ChainHeaderReader) (*Snapshot, error) {
	// Allow passing in no headers for cleaner code
	if len(headers) == 0 {
		return s, nil
	}
	// Sanity check that the headers can be applied
	for i := 0; i < len(headers)-1; i++ {
		if headers[i+1].Number.Uint64() != headers[i].Number.Uint64()+1 {
			return nil, errInvalidVotingChain
		}
	}
	if headers[0].Number.Uint64() != s.Number+1 {
		return nil, errInvalidVotingChain
	}
	// Iterate through the headers and create a new snapshot
	snap := s.copy()

	for _, header := range headers {
		// Resolve the authorization key and check against signers
		coinbase, err := ecrecover(header, s.sigcache)
		if err != nil {
			return nil, err
		}
		if coinbase.String() != header.Coinbase.String() && header.Number.Cmp(big.NewInt(bugFixBlockNumber)) != 0 {
			return nil, errUnauthorized
		}

		headerExtra := HeaderExtra{}
		err = decodeHeaderExtra(s.config, header.Number, header.Extra[extraVanity:len(header.Extra)-extraSeal], &headerExtra)
		if err != nil {
			return nil, err
		}
		snap.HeaderTime = header.Time
		snap.LoopStartTime = headerExtra.LoopStartTime
		snap.Signers = nil
		for i := range headerExtra.SignerQueue {
			snap.Signers = append(snap.Signers, &headerExtra.SignerQueue[i])
		}

		snap.ConfirmedNumber = headerExtra.ConfirmedBlockNumber

		if len(snap.HistoryHash) >= int(s.config.MaxSignerCount)*2 {
			snap.HistoryHash = snap.HistoryHash[1 : int(s.config.MaxSignerCount)*2]
		}
		snap.HistoryHash = append(snap.HistoryHash, header.Hash())

		// deal the new confirmation in this block
		snap.updateSnapshotByConfirmations(headerExtra.CurrentBlockConfirmations)

		// deal the new vote from voter
		snap.updateSnapshotByVotes(headerExtra.CurrentBlockVotes, header.Number)

		// deal the voter which balance modified
		snap.updateSnapshotByMPVotes(headerExtra.ModifyPredecessorVotes, header.Number.Uint64())

		// deal the snap related with punished
		snap.updateSnapshotForPunish(headerExtra.SignerMissing, header.Number, header.Coinbase)

		// deal proposals
		snap.updateSnapshotByProposals(headerExtra.CurrentBlockProposals, header.Number)

		// deal declares
		snap.updateSnapshotByDeclares(headerExtra.CurrentBlockDeclares, header.Number)

		// deal trantor upgrade
		if snap.Period == 0 {
			snap.Period = snap.config.Period
		}

		// deal setcoinbase for side chain
		snap.updateSnapshotBySetSCCoinbase(headerExtra.SideChainSetCoinbases)

		// deal confirmation for side chain
		snap.updateSnapshotBySCConfirm(headerExtra.SideChainConfirmations, header.Number)

		// deal notice confirmation
		snap.updateSnapshotByNoticeConfirm(headerExtra.SideChainNoticeConfirmed, header.Number)

		// calculate proposal result
		snap.calculateProposalResult(header.Number)

		// check the len of candidate if not candidateNeedPD
		if !candidateNeedPD && (snap.Number+1)%(snap.config.MaxSignerCount*snap.LCRS) == 0 && len(snap.Candidates) > candidateMaxLen {
			snap.removeExtraCandidate()
		}

		/*
		 * follow methods only work on side chain !!!! not like above method
		 */

		// deal the notice from main chain
		snap.updateSnapshotBySCCharging(headerExtra.SideChainCharging, header.Number, header.Coinbase)

		snap.updateSnapshotForExpired(header.Number)

		snap.updateFlowMiner(header, db)
		snap.updateMinerStack(headerExtra.MinerStake, header.Number.Uint64())

		if header.Number.Uint64() < PosrIncentiveEffectNumber {
			snap.updateGrantProfit(headerExtra.GrantProfit, db, header.Hash(), header.Number.Uint64())
		} else {
			err = snap.updateGrantProfit2(headerExtra.GrantProfitHash, db, header)
			if err != nil {
				return nil, err
			}
		}
		if header.Number.Uint64() == lockMergeNumber {
			snap.FlowRevenue.updateMergeLockData(db, snap.Period, snap.Hash)
		}
		snap.updateFlowRevenueRls(headerExtra.LockReward, header.Number)
		snap.updateExchangeNFC(headerExtra.ExchangeNFC)
		snap.updateDeviceBind(headerExtra.DeviceBind, header.Number.Uint64())
		snap.updateCandidatePledge(headerExtra.CandidatePledge)
		snap.updateCandidatePunish(headerExtra.CandidatePunish, header.Number.Uint64())
		snap.updateCandidateExit(headerExtra.CandidateExit, header.Number)
		snap.updateClaimedBandwidth(headerExtra.ClaimedBandwidth)
		snap.updateFlowMinerExit(headerExtra.FlowMinerExit, header.Number)
		snap.updateBandwidthPunish(headerExtra.BandwidthPunish)
		snap.updateFlowReport(headerExtra.FlowReport, header.Number)
		snap.updateConfigExchRate(headerExtra.ConfigExchRate)
		snap.updateConfigOffLine(headerExtra.ConfigOffLine)
		snap.updateConfigDeposit(headerExtra.ConfigDeposit)
		snap.updateFlowHarvest(headerExtra.FlowHarvest)
		snap.updateConfigISPQOS(headerExtra.ConfigISPQOS)
		snap.updateManagerAddress(headerExtra.ManagerAddress)
		snap.updateLockParameters(headerExtra.LockParameters)
		if header.Number.Uint64()%(snap.config.MaxSignerCount*snap.LCRS) == 0 && header.Number.Uint64() >= signFixBlockNumber {
			snap.updateSignerNumber(headerExtra.SignerQueue, header.Number.Uint64())
		}
		if header.Number.Uint64() >= StorageEffectBlockNumber {
			reSnap, err := snap.storageApply(headerExtra, header, db)
			if err != nil {
				log.Error("snap.storageApply", "err", err)
				return reSnap, nil
			}
		}
		if header.Number.Uint64() >= initStorageManagerNumber {
			reSnap, err := snap.sPApply(headerExtra, header, db)
			if err != nil {
				log.Error("snap.sPApply", "err", err)
				return reSnap, nil
			}
		}
		if header.Number.Uint64() == (StorageEffectBlockNumber - 1) {
			snap.StorageData = NewStorageSnap()
		}
		if header.Number.Uint64() >= StorageChBwEffectNumber {
			snap.updateStorageBandWidth(headerExtra.StorageExchangeBw, header.Number, nil)
		}
		if header.Number.Uint64() == (PledgeRevertLockEffectNumber - 1) {
			snap.SRT, err = NewSRT(common.Hash{}, db)
			if err != nil {
				return snap, nil
			}
			snap.FlowRevenue.PosPgExitLock = NewLockData(LOCKPOSEXITDATA)
		}
		if header.Number.Uint64() == StoragePledgeOptEffectNumber {
			snap.initBandwidthMakeup(header.Number)
		}
		if header.Number.Uint64() == PosrIncentiveEffectNumber {
			snap.initBandwidthMakeup2(header.Number)
		}
		if header.Number.Uint64() == (PosrIncentiveEffectNumber + BandwidthAdjustPeriodDay*snap.getBlockPreDay()) {
			snap.setBandwidthMakeupPunish(header, db)
		}
		if header.Number.Uint64() == PosrNewCalEffectNumber {
			snap.fixStorageRevertRevenue(header, db)
		}
		if header.Number.Uint64() == (PosNewEffectNumber - 1) {
			snap.PosPledge = make(map[common.Address]*PosPledgeItem, 0)
			snap.FlowRevenue.PosExitLock = NewLockData(LOCKPEXITDATA)
			snap.initPosPledge(header.Number.Uint64())
			snap.initPosExitPunish(header, db)
		}
		if isGEPOSNewEffect(header.Number.Uint64()) {
			snap.updateCandidatePledgeNew(headerExtra.CandidatePledgeNew, header.Number.Uint64())
			snap.updateCandidatePledgeEntrust(headerExtra.CandidatePledgeEntrust, header.Number.Uint64())
			snap.updateCandidatePEntrustExit(headerExtra.CandidatePEntrustExit, header.Number)
			snap.updateCandidateAutoExit(headerExtra.CandidateAutoExit, header, db)
			snap.updateCandidateChangeRate(headerExtra.CandidateChangeRate, header, db)
			if len(headerExtra.MinerStake) > 0 || len(headerExtra.ModifyPredecessorVotes) > 0 {
				snap.deletePunishByPosExit(header.Number.Uint64())
			}
			snap.updateCandidateExit2(headerExtra.CandidateExit, header.Number)
		}
		if header.Number.Uint64() == PosLastPunishFixNumber {
			snap.initPosExitPunishFix()
		}
		if header.Number.Uint64() == PoCrsAccCalNumber-1 {
			snap.TotalLeaseSpace = new(big.Int).Set(initTotalLeaseSpace)
		}
		if isGEPoCrsAccCalNumber(header.Number.Uint64()) {
			snap.updateTotalLeaseSpace(headerExtra.CurLeaseSpace)
		}
		if header.Number.Uint64() == (initStorageManagerNumber - 1) {
			snap.initStorageManager()
			snap.FlowRevenue.STPEntrustExitLock = NewLockData(LOCKSTPEEXITDATA)
			snap.FlowRevenue.STPEntrustLock = NewLockData(LOCKSTPEDATA)
			snap.FlowRevenue.SpLock = NewLockData(LOCKSPLOCKDATA)
			snap.FlowRevenue.SpEntrustLock = NewLockData(LOCKSPETTTDATA)
			snap.FlowRevenue.SpExitLock = NewLockData(LOCKSPEXITDATA)
			snap.FlowRevenue.SpEntrustExitLock = NewLockData(LOCKSPETTEXITDATA)
			snap.SpData = NewSPSnap()
			snap.initSpData(header.Number.Uint64())
		}

		if isGEInitStorageManagerNumber(header.Number.Uint64()){
			snap.updatePOSTransfer(headerExtra.POSTransfer,header.Number)
		}
	}
	snap.Number += uint64(len(headers))
	snap.Hash = headers[len(headers)-1].Hash()
	snap.FlowRevenue.Number = snap.Number
	snap.FlowRevenue.Hash = snap.Hash
	err := snap.verifyTallyCnt()
	if err != nil {
		return nil, err
	}
	if snap.SRT != nil {
		snap.SRTHash, err = snap.SRT.Save(db)
		if err != nil {
			return nil, err
		}
	}
	return snap, nil
}

func (snap *Snapshot) updateLockParameters(lockParameters []LockParameterRecord) {
	for _, item := range lockParameters {
		if _, ok := snap.SystemConfig.LockParameters[item.Who]; ok {
			snap.SystemConfig.LockParameters[item.Who].LockPeriod = item.LockPeriod
			snap.SystemConfig.LockParameters[item.Who].RlsPeriod = item.RlsPeriod
			snap.SystemConfig.LockParameters[item.Who].Interval = item.Interval
		} else {
			snap.SystemConfig.LockParameters[item.Who] = &LockParameter{
				LockPeriod: item.LockPeriod,
				RlsPeriod:  item.RlsPeriod,
				Interval:   item.Interval,
			}
		}
	}
}

func (snap *Snapshot) updateManagerAddress(managerAddress []ManagerAddressRecord) {
	for _, item := range managerAddress {
		snap.SystemConfig.ManagerAddress[item.Who] = item.Target
	}
}

func (snap *Snapshot) updateConfigISPQOS(configISPQOS []ISPQOSRecord) {
	for _, item := range configISPQOS {
		snap.SystemConfig.QosConfig[item.ISPID] = item.QOS
	}
}

func (snap *Snapshot) updateFlowHarvest(flowHarvest *big.Int) {
	if 0 < flowHarvest.Cmp(big.NewInt(0)) {
		if nil == snap.FlowHarvest {
			snap.FlowHarvest = new(big.Int).Set(flowHarvest)
		} else {
			snap.FlowHarvest = new(big.Int).Add(snap.FlowHarvest, flowHarvest)
		}
	}
}

func (snap *Snapshot) updateConfigDeposit(configDeposit []ConfigDepositRecord) {
	for _, item := range configDeposit {
		if 0 < item.Amount.Cmp(big.NewInt(0)) {
			snap.SystemConfig.Deposit[item.Who] = new(big.Int).Set(item.Amount)
		}
	}
}

func (snap *Snapshot) updateConfigOffLine(configOffLine uint32) {
	if 0 < configOffLine {
		snap.SystemConfig.OffLine = configOffLine
	}
}

func (snap *Snapshot) updateConfigExchRate(configExchRate uint32) {
	if 0 < configExchRate {
		snap.SystemConfig.ExchRate = configExchRate
	}
}

func (snap *Snapshot) getFlowRewardBlock() uint64 {
	return accumulateFlowRewardInterval / snap.config.Period
}
func (snap *Snapshot) getBlockPreDay() uint64 {
	return secondsPerDay / snap.config.Period
}

func (snap *Snapshot) updateFlowReport(flowReport []MinerFlowReportRecord, headerNumber *big.Int) {
	snap.FlowMiner.updateFlowReport(snap.getFlowRewardBlock(), snap.getBlockPreDay(), flowReport, headerNumber)
	snap.updateFULBalanceCostTotal(flowReport)
}

func (snap *Snapshot) updateFULBalanceCostTotal(flowReport []MinerFlowReportRecord) {
	fulRatioZero := big.NewInt(1e5)
	for _, items := range flowReport {
		sc := items.ChainHash
		for _, flow := range items.ReportContent {
			if flow.FlowValue2 > 0 {
				address := flow.Target
				if _, ok := snap.FULBalance[address]; !ok {
					snap.FULBalance[address] = &FULLBalanceData{
						Balance:   big.NewInt(0),
						CostTotal: make(map[common.Hash]*big.Int),
					}
				}
				cost := new(big.Int).Mul(new(big.Int).SetUint64(flow.FlowValue2), fulRatioZero)
				if _, ok := snap.FULBalance[address].CostTotal[sc]; !ok {
					snap.FULBalance[address].CostTotal[sc] = cost
				} else {
					snap.FULBalance[address].CostTotal[sc] = new(big.Int).Add(snap.FULBalance[address].CostTotal[sc], cost)
				}
			}
		}
	}
}

func (snap *Snapshot) updateBandwidthPunish(bandwidthPunish []BandwidthPunishRecord) {
	for _, item := range bandwidthPunish {
		if _, ok := snap.Bandwidth[item.Target]; ok {
			snap.Bandwidth[item.Target].BandwidthClaimed = item.WdthPnsh
		}
	}
}

func (snap *Snapshot) updateFlowMinerExit(flowMinerExit []common.Address, headerNumber *big.Int) {
	for _, item := range flowMinerExit {
		if _, ok := snap.FlowPledge[item]; ok {
			snap.FlowPledge[item].LockPeriod = snap.SystemConfig.LockParameters[sscEnumFlwLock].LockPeriod
			snap.FlowPledge[item].RlsPeriod = snap.SystemConfig.LockParameters[sscEnumFlwLock].RlsPeriod
			snap.FlowPledge[item].Interval = snap.SystemConfig.LockParameters[sscEnumFlwLock].Interval
			snap.FlowPledge[item].StartHigh = headerNumber.Uint64()
			snap.FlowPledge[item].TargetAddress = item
			if revenue, ok := snap.RevenueFlow[item]; ok {
				snap.FlowPledge[item].RevenueAddress = revenue.RevenueAddress
				snap.FlowPledge[item].RevenueContract = revenue.RevenueContract
				snap.FlowPledge[item].MultiSignature = revenue.MultiSignature
			} else {
				snap.FlowPledge[item].RevenueAddress = item
				snap.FlowPledge[item].RevenueContract = common.Address{}
				snap.FlowPledge[item].MultiSignature = common.Address{}
			}
		}
		delete(snap.Bandwidth, item)
	}
}

func (snap *Snapshot) updateClaimedBandwidth(claimedBandwidth []ClaimedBandwidthRecord) {
	for _, item := range claimedBandwidth {
		if _, ok := snap.FlowPledge[item.Target]; ok {
			snap.FlowPledge[item.Target].Amount = new(big.Int).Add(snap.FlowPledge[item.Target].Amount, item.Amount)
		} else {
			snap.FlowPledge[item.Target] = NewPledgeItem(item.Amount)
		}
		if _, ok := snap.Bandwidth[item.Target]; ok {
			snap.Bandwidth[item.Target].BandwidthClaimed = item.Bandwidth
		} else {
			snap.Bandwidth[item.Target] = &ClaimedBandwidth{
				ISPQosID:         item.ISPQosID,
				BandwidthClaimed: item.Bandwidth,
			}
		}
	}
}

func (snap *Snapshot) updateCandidateExit(candidateExit []common.Address, headerNumber *big.Int) {
	if isGEPOSNewEffect(headerNumber.Uint64()) {
		return
	}
	for _, item := range candidateExit {
		if _, ok := snap.CandidatePledge[item]; ok {
			snap.CandidatePledge[item].LockPeriod = snap.SystemConfig.LockParameters[sscEnumCndLock].LockPeriod
			snap.CandidatePledge[item].RlsPeriod = snap.SystemConfig.LockParameters[sscEnumCndLock].RlsPeriod
			snap.CandidatePledge[item].Interval = snap.SystemConfig.LockParameters[sscEnumCndLock].Interval
			snap.CandidatePledge[item].StartHigh = headerNumber.Uint64()
			snap.CandidatePledge[item].TargetAddress = item
			if revenue, ok := snap.RevenueNormal[item]; ok {
				snap.CandidatePledge[item].RevenueAddress = revenue.RevenueAddress
				snap.CandidatePledge[item].RevenueContract = revenue.RevenueContract
				snap.CandidatePledge[item].MultiSignature = revenue.MultiSignature
			} else {
				snap.CandidatePledge[item].RevenueAddress = item
				snap.CandidatePledge[item].RevenueContract = common.Address{}
				snap.CandidatePledge[item].MultiSignature = common.Address{}
			}
		}
		if _, ok := snap.TallyMiner[item]; ok {
			delete(snap.TallyMiner, item)
		}
	}
}

func (snap *Snapshot) updateCandidatePunish(candidatePunish []CandidatePunishRecord, number uint64) {
	for _, item := range candidatePunish {
		if _, ok := snap.Punished[item.Target]; ok {
			if snap.Punished[item.Target] > uint64(item.Credit) {
				snap.Punished[item.Target] -= uint64(item.Credit)
			} else {
				delete(snap.Punished, item.Target)
				if isGEPOSNewEffect(number) {
					if _, ok2 := snap.PosPledge[item.Target]; ok2 {
						snap.PosPledge[item.Target].LastPunish = 0
					}
				}
			}
		}
		if !isGEPOSNewEffect(number) {
			if _, ok := snap.CandidatePledge[item.Target]; ok {
				snap.CandidatePledge[item.Target].Amount = new(big.Int).Add(snap.CandidatePledge[item.Target].Amount, item.Amount)
			} else {
				snap.CandidatePledge[item.Target] = NewPledgeItem(item.Amount)
			}
		}
	}
}

func (snap *Snapshot) updateCandidatePledge(candidatePledge []CandidatePledgeRecord) {
	for _, item := range candidatePledge {
		if _, ok := snap.CandidatePledge[item.Target]; ok {
			snap.CandidatePledge[item.Target].Amount = new(big.Int).Add(snap.CandidatePledge[item.Target].Amount, item.Amount)
		} else {
			snap.CandidatePledge[item.Target] = NewPledgeItem(item.Amount)
		}
		if _, ok := snap.TallyMiner[item.Target]; !ok {
			snap.TallyMiner[item.Target] = &CandidateState{
				SignerNumber: 0,
				Stake:        big.NewInt(0),
			}
		}
	}
}

func (snap *Snapshot) updateDeviceBind(deviceBind []DeviceBindRecord, headerNumber uint64) {
	for _, item := range deviceBind {
		if item.Type == 0 {
			if item.Bind {
				snap.RevenueNormal[item.Device] = &RevenueParameter{
					RevenueAddress:  item.Revenue,
					RevenueContract: item.Contract,
					MultiSignature:  item.MultiSign,
				}
			} else {
				delete(snap.RevenueNormal, item.Device)
			}
		} else {
			if headerNumber >= StorageEffectBlockNumber {
				if item.Bind {
					snap.RevenueStorage[item.Device] = &RevenueParameter{
						RevenueAddress:  item.Revenue,
						RevenueContract: item.Contract,
						MultiSignature:  item.MultiSign,
					}
				} else {
					delete(snap.RevenueStorage, item.Device)
				}
			} else {
				if item.Bind {
					snap.RevenueFlow[item.Device] = &RevenueParameter{
						RevenueAddress:  item.Revenue,
						RevenueContract: item.Contract,
						MultiSignature:  item.MultiSign,
					}
				} else {
					delete(snap.RevenueFlow, item.Device)
				}
			}

		}
	}
}

func (snap *Snapshot) updateExchangeNFC(exchangeNFC []ExchangeNFCRecord) {
	for _, item := range exchangeNFC {
		if balance, ok := snap.FULBalance[item.Target]; !ok {
			snap.FULBalance[item.Target] = &FULLBalanceData{
				Balance:   new(big.Int).Set(item.Amount),
				CostTotal: make(map[common.Hash]*big.Int),
			}
		} else {
			balance.Balance = new(big.Int).Add(balance.Balance, item.Amount)
		}
	}
}

func (snap *Snapshot) updateGrantProfit(grantProfit []consensus.GrantProfitRecord, db ethdb.Database, headerHash common.Hash, number uint64) {
	for _, item := range grantProfit {
		if 0 == item.BlockNumber {
			if sscEnumCndLock == item.Which {
				if pledge, ok := snap.CandidatePledge[item.MinerAddress]; ok {
					pledge.Playment = new(big.Int).Add(pledge.Playment, item.Amount)
					if 0 <= pledge.Playment.Cmp(pledge.Amount) {
						delete(snap.CandidatePledge, item.MinerAddress)
					}
				}
			} else if sscEnumFlwLock == item.Which {
				if pledge, ok := snap.FlowPledge[item.MinerAddress]; ok {
					pledge.Playment = new(big.Int).Add(pledge.Playment, item.Amount)
					if 0 <= pledge.Playment.Cmp(pledge.Amount) {
						delete(snap.FlowPledge, item.MinerAddress)
					}
				}
			}
		}
	}
	snap.FlowRevenue.updateGrantProfit(grantProfit, db, headerHash, number)
}

func (snap *Snapshot) updateMinerStack(minerStake []MinerStakeRecord, headerNumber uint64) {
	for _, item := range minerStake {
		if _, ok := snap.TallyMiner[item.Target]; ok {
			snap.TallyMiner[item.Target].Stake = new(big.Int).Set(item.Stake)
		} else {
			snap.TallyMiner[item.Target] = &CandidateState{
				SignerNumber: 0,
				Stake:        new(big.Int).Set(item.Stake),
			}
		}
		snap.checkPosPledgePunish(item.Target, headerNumber)

	}
}

func (snap *Snapshot) updateFlowMiner(header *types.Header, db ethdb.Database) {
	rewardBlock := snap.getFlowRewardBlock()
	blockPerDay := snap.getBlockPreDay()
	if 0 == header.Number.Uint64()%blockPerDay && 0 != header.Number.Uint64() {
		snap.FlowMiner.updateFlowMinerDaily(blockPerDay, header)
	} else if rewardBlock == header.Number.Uint64()%blockPerDay && rewardBlock != header.Number.Uint64() {
		totalFlow := big.NewInt(0)
		flowcensus := snap.FlowMiner.accumulateFlows(db)
		for minerAddress, bandwidth := range flowcensus {
			if claimed, ok := snap.Bandwidth[minerAddress]; ok {
				validFlow := bandwidth.FlowValue1
				if validFlow <= 0 {
					continue
				}
				bandwidthHigh := uint64(claimed.BandwidthClaimed) * uint64(24*60*60)
				if bandwidth.FlowValue1 > bandwidthHigh {
					validFlow = bandwidthHigh
				}
				totalFlow = new(big.Int).Add(totalFlow, big.NewInt(int64(validFlow)))
			}
		}
		if nil == snap.FlowTotal {
			snap.FlowTotal = totalFlow
		} else {
			snap.FlowTotal = new(big.Int).Add(snap.FlowTotal, totalFlow)
		}
		snap.FlowMiner.cleanPrevFlow()
	}
}

func (s *Snapshot) updateFlowRevenueRls(LockReward []LockRewardRecord, headerNumber *big.Int) {
	s.FlowRevenue.updateLockData(s, LockReward, headerNumber)
	s.FlowRevenue.updateAllLockDataNew(s,headerNumber)

}

func (s *Snapshot) removeExtraCandidate() {
	// remove minimum tickets tally beyond candidateMaxLen
	tallySlice := s.buildTallySlice()
	sort.Sort(TallySlice(tallySlice))
	if len(tallySlice) > candidateMaxLen {
		removeNeedTally := tallySlice[candidateMaxLen:]
		for _, tallySlice := range removeNeedTally {
			//if _, ok := s.SCCoinbase[tallySlice.addr]; ok {
			//	delete(s.SCCoinbase, tallySlice.addr)
			//}
			delete(s.Candidates, tallySlice.addr)
		}
	}
}

func (s *Snapshot) verifyTallyCnt() error {

	tallyTarget := make(map[common.Address]*big.Int)
	for _, v := range s.Votes {
		if _, ok := tallyTarget[v.Candidate]; ok {
			tallyTarget[v.Candidate].Add(tallyTarget[v.Candidate], v.Stake)
		} else {
			tallyTarget[v.Candidate] = new(big.Int).Set(v.Stake)
		}
	}

	for address, tally := range s.Tally {
		if targetTally, ok := tallyTarget[address]; ok && targetTally.Cmp(tally) == 0 {
			continue
		} else {
			return errIncorrectTallyCount
		}
	}

	return nil
}

func (s *Snapshot) updateSnapshotBySetSCCoinbase(scCoinbases []SCSetCoinbase) {
	for _, scc := range scCoinbases {
		//if _, ok := s.SCCoinbase[scc.Signer]; !ok {
		//	s.SCCoinbase[scc.Signer] = make(map[common.Hash]common.Address)
		//}
		//s.SCCoinbase[scc.Signer][scc.Hash] = scc.Coinbase
		if scc.Type {
			if _, ok := s.SCCoinbase[scc.Hash]; !ok {
				s.SCCoinbase[scc.Hash] = make(map[common.Address]common.Address)
			}
			s.SCCoinbase[scc.Hash][scc.Coinbase] = scc.Signer
		} else {
			if _, ok := s.SCCoinbase[scc.Hash]; ok {
				delete(s.SCCoinbase[scc.Hash], scc.Coinbase)
				if 0 == len(s.SCCoinbase[scc.Hash]) {
					delete(s.SCCoinbase, scc.Hash)
				}
			}
		}
	}
}

func (s *Snapshot) isSideChainCoinbase(sc common.Hash, address common.Address, realtime bool) bool {
	// check is side chain coinbase
	// is use the coinbase of main chain as coinbase of side chain , return false
	// the main chain cloud seal block, but not recommend for send confirm tx usually fail
	//if realtime {
	//	for _, signer := range s.Signers {
	//		if _, ok := s.SCCoinbase[*signer]; ok {
	//			if coinbase, ok := s.SCCoinbase[*signer][sc]; ok && coinbase == address {
	//				return true
	//			}
	//		}
	//	}
	//} else {
	//	for _, coinbaseMap := range s.SCCoinbase {
	//		if coinbase, ok := coinbaseMap[sc]; ok && coinbase == address {
	//			return true
	//		}
	//	}
	//}
	if coinbaseMap, ok := s.SCCoinbase[sc]; ok {
		if _, ok = coinbaseMap[address]; ok {
			return true
		}
	}
	return false
}

func (s *Snapshot) updateSnapshotBySCConfirm(scConfirmations []SCConfirmation, headerNumber *big.Int) {
	// todo ,if diff side chain coinbase send confirm for the same side chain , same number ...
	for _, scc := range scConfirmations {
		// new confirmation header number must larger than last confirmed number of this side chain
		if s.isSideChainCoinbase(scc.Hash, scc.Coinbase, false) {
			if _, ok := s.SCRecordMap[scc.Hash]; ok && scc.Number > s.SCRecordMap[scc.Hash].LastConfirmedNumber {
				s.SCRecordMap[scc.Hash].Record[scc.Number] = append(s.SCRecordMap[scc.Hash].Record[scc.Number], scc.copy())
				if scc.Number > s.SCRecordMap[scc.Hash].MaxHeaderNumber {
					s.SCRecordMap[scc.Hash].MaxHeaderNumber = scc.Number
				}
			}
		}
	}
	// calculate the side chain reward in each loop
	if (headerNumber.Uint64()+1)%s.config.MaxSignerCount == 0 {
		s.checkSCConfirmation(headerNumber)
		s.updateSCConfirmation(headerNumber)
	}
}

func (s *Snapshot) updateSnapshotByNoticeConfirm(scNoticeConfirmed []SCConfirmation, headerNumber *big.Int) {
	// record the confirmed info into Notice, and remove notice if there are enough confirm
	// may be receive confirmed more than 2/3+1 and the remove will delay a reasonable loop count (4)
	for _, noticeConfirm := range scNoticeConfirmed {
		// check if the coinbase of this side chain
		// todo check if the current coinbase of this side chain.
		if !s.isSideChainCoinbase(noticeConfirm.Hash, noticeConfirm.Coinbase, true) {
			continue
		}
		// noticeConfirm.Hash is the hash of side chain
		if _, ok := s.SCNoticeMap[noticeConfirm.Hash]; ok {
			for _, strHash := range noticeConfirm.LoopInfo {
				// check the charging current exist
				noticeHash := common.HexToHash(strHash)
				if _, ok := s.SCNoticeMap[noticeConfirm.Hash].CurrentCharging[noticeHash]; ok {
					//noticeType = noticeTypeGasCharging
					if _, ok := s.SCNoticeMap[noticeConfirm.Hash].ConfirmReceived[noticeHash]; !ok {
						s.SCNoticeMap[noticeConfirm.Hash].ConfirmReceived[noticeHash] = NoticeCR{make(map[common.Address]bool), 0, noticeTypeGasCharging, false}
					}
					s.SCNoticeMap[noticeConfirm.Hash].ConfirmReceived[noticeHash].NRecord[noticeConfirm.Coinbase] = true
				}
			}
		}
	}

	// check notice confirm number
	if (headerNumber.Uint64()+1)%s.config.MaxSignerCount == 0 {
		// todo : check if the enough coinbase is the side chain coinbase which main chain coinbase is in the signers
		// todo : if checked ,then update the number in noticeConfirmed
		// todo : remove the notice , delete(notice,hash) to stop the broadcast to side chain

		for chainHash, scNotice := range s.SCNoticeMap {
			// check each side chain
			for noticeHash, noticeRecord := range scNotice.ConfirmReceived {
				if len(noticeRecord.NRecord) >= int(2*s.config.MaxSignerCount/3+1) && !noticeRecord.Success {
					s.SCNoticeMap[chainHash].ConfirmReceived[noticeHash] = NoticeCR{noticeRecord.NRecord, headerNumber.Uint64(), noticeRecord.Type, true}
				}

				if noticeRecord.Success && noticeRecord.Number < headerNumber.Uint64()-s.config.MaxSignerCount*mcNoticeClearDelayLoopCount {
					delete(s.SCNoticeMap[chainHash].CurrentCharging, noticeHash)
					delete(s.SCNoticeMap[chainHash].ConfirmReceived, noticeHash)
				}
			}
		}
	}

}

func (s *Snapshot) updateSnapshotBySCCharging(scCharging []GasCharging, headerNumber *big.Int, coinbase common.Address) {
	for _, charge := range scCharging {
		if _, ok := s.LocalNotice.CurrentCharging[charge.Hash]; !ok {
			s.LocalNotice.CurrentCharging[charge.Hash] = GasCharging{charge.Target, charge.Volume, charge.Hash}
			s.LocalNotice.ConfirmReceived[charge.Hash] = NoticeCR{make(map[common.Address]bool), 0, noticeTypeGasCharging, false}

		}
		s.LocalNotice.ConfirmReceived[charge.Hash].NRecord[coinbase] = true
	}

	if (headerNumber.Uint64()+1)%s.config.MaxSignerCount == 0 {
		for hash, noticeRecord := range s.LocalNotice.ConfirmReceived {
			if len(noticeRecord.NRecord) >= int(2*s.config.MaxSignerCount/3+1) && !noticeRecord.Success {
				s.LocalNotice.ConfirmReceived[hash] = NoticeCR{noticeRecord.NRecord, headerNumber.Uint64(), noticeTypeGasCharging, true}
				// todo charging the gas fee on set block

			}
			if noticeRecord.Success && noticeRecord.Number < headerNumber.Uint64()-s.config.MaxSignerCount*scNoticeClearDelayLoopCount {
				delete(s.LocalNotice.CurrentCharging, hash)
				delete(s.LocalNotice.ConfirmReceived, hash)
			}
		}
	}

}

func (s *Snapshot) checkSCConfirmation(headerNumber *big.Int) {
	for hash, scRecord := range s.SCRecordMap {
		// check maxRentRewardNumber by headerNumber
		for txHash, scRentInfo := range scRecord.RentReward {
			if scRentInfo.MaxRewardNumber.Uint64() < headerNumber.Uint64()-scRewardExpiredLoopCount*s.config.MaxSignerCount {
				delete(s.SCRecordMap[hash].RentReward, txHash)
			}
		}

		// if size of confirmed record from one side chain larger than scMaxConfirmedRecordLength
		// we reset the record info of this side chain, good enough for now
		if len(scRecord.Record) > scMaxConfirmedRecordLength {
			s.SCRecordMap[hash].Record = make(map[uint64][]*SCConfirmation)
			s.SCRecordMap[hash].LastConfirmedNumber = 0
			s.SCRecordMap[hash].MaxHeaderNumber = 0
			// the rentReward info will be kept, do not delete
		}
	}

}

func (s *Snapshot) calculateSCConfirmedNumber(record *SCRecord, minConfirmedSignerCount int) (uint64, map[uint64]common.Address) {
	// todo : add params scHash, so can check if the address in SCRecord is belong to this side chain

	confirmedNumber := record.LastConfirmedNumber
	confirmedRecordMap := make(map[string]map[common.Address]bool)
	confirmedCoinbase := make(map[uint64]common.Address)
	sep := ":"
	tmpHeaderNum := new(big.Int)
	for i := record.LastConfirmedNumber + 1; i <= record.MaxHeaderNumber; i++ {
		if _, ok := record.Record[i]; ok {
			// during reorged, the side chain loop info may more than one for each side chain block number.
			for _, scConfirm := range record.Record[i] {
				// loopInfo slice contain number and coinbase address of side chain block,
				// so the length of loop info must larger than twice of minConfirmedSignerCount .
				if len(scConfirm.LoopInfo) >= minConfirmedSignerCount*2 {
					key := strings.Join(scConfirm.LoopInfo, sep)
					if _, ok := confirmedRecordMap[key]; !ok {
						confirmedRecordMap[key] = make(map[common.Address]bool)
					}
					// new coinbase for same loop info
					if _, ok := confirmedRecordMap[key][scConfirm.Coinbase]; !ok {
						confirmedRecordMap[key][scConfirm.Coinbase] = true
						if len(confirmedRecordMap[key]) >= minConfirmedSignerCount {
							err := tmpHeaderNum.UnmarshalText([]byte(scConfirm.LoopInfo[len(scConfirm.LoopInfo)-2]))
							if err == nil && tmpHeaderNum.Uint64() > confirmedNumber {
								confirmedNumber = tmpHeaderNum.Uint64()
							}
						}
					}
				}
			}
		}
	}

	for info, confirm := range confirmedRecordMap {
		if len(confirm) >= minConfirmedSignerCount {
			infos := strings.Split(info, sep)
			for i := 0; i+1 < len(infos); i += 2 {
				err := tmpHeaderNum.UnmarshalText([]byte(infos[i]))
				if err != nil {
					continue
				}
				confirmedCoinbase[tmpHeaderNum.Uint64()] = common.HexToAddress(infos[i+1])
			}
		}
	}

	// for calculate side chain reward
	// if the side chain count per period is more than one
	// then the reward should calculate continue till one coinbase finished.
	if record.CountPerPeriod > 1 && confirmedNumber > record.LastConfirmedNumber {
		if lastConfirmedCoinbase, ok := confirmedCoinbase[confirmedNumber]; ok {
			for i := confirmedNumber - 1; i > confirmedNumber-record.CountPerPeriod; i-- {
				if lastConfirmedCoinbase != confirmedCoinbase[i] {
					confirmedNumber = i
					break
				}
			}
			for i := confirmedNumber + 1; i < confirmedNumber+record.CountPerPeriod; i++ {
				if _, ok = confirmedCoinbase[i]; ok {
					delete(confirmedCoinbase, i)
				}
			}
		}
	}

	return confirmedNumber, confirmedCoinbase
}

func (s *Snapshot) calculateCurrentBlockReward(currentCount uint64, periodCount uint64) uint64 {
	currentRewardPercentage := uint64(0)
	if periodCount > uint64(scMaxCountPerPeriod) {
		periodCount = scMaxCountPerPeriod
	}
	if v, ok := SCCurrentBlockReward[periodCount][currentCount]; ok {
		currentRewardPercentage = v
	}
	return currentRewardPercentage
}

func (s *Snapshot) updateSCConfirmation(headerNumber *big.Int) {
	minConfirmedSignerCount := int(2 * s.config.MaxSignerCount / 3)
	for scHash, record := range s.SCRecordMap {
		if _, ok := s.SCRewardMap[scHash]; !ok {
			s.SCRewardMap[scHash] = &SCReward{SCBlockRewardMap: make(map[uint64]*SCBlockReward)}
		}
		currentReward := &SCBlockReward{RewardScoreMap: make(map[common.Address]uint64)}
		confirmedNumber, confirmedCoinbase := s.calculateSCConfirmedNumber(record, minConfirmedSignerCount)
		if confirmedNumber > record.LastConfirmedNumber {
			// todo: map coinbase of side chain to coin base of main chain here
			lastSCCoinbase := common.Address{}
			currentSCCoinbaseCount := uint64(0)
			for n := record.LastConfirmedNumber + 1; n <= confirmedNumber; n++ {
				if scCoinbase, ok := confirmedCoinbase[n]; ok {
					// if scCoinbase not same with lastSCCoinbase recount
					if lastSCCoinbase != scCoinbase {
						currentSCCoinbaseCount = 1
					} else {
						currentSCCoinbaseCount++
					}

					if _, ok := currentReward.RewardScoreMap[scCoinbase]; !ok {
						currentReward.RewardScoreMap[scCoinbase] = s.calculateCurrentBlockReward(currentSCCoinbaseCount, record.CountPerPeriod)
					} else {
						currentReward.RewardScoreMap[scCoinbase] += s.calculateCurrentBlockReward(currentSCCoinbaseCount, record.CountPerPeriod)
					}

					// update lastSCCoinbase
					lastSCCoinbase = scCoinbase
				}
			}

			for i := record.LastConfirmedNumber + 1; i <= confirmedNumber; i++ {
				if _, ok := s.SCRecordMap[scHash].Record[i]; ok {
					delete(s.SCRecordMap[scHash].Record, i)
				}
			}
			s.SCRecordMap[scHash].LastConfirmedNumber = confirmedNumber
		}
		// clear empty block number for side chain
		if len(currentReward.RewardScoreMap) != 0 {
			s.SCRewardMap[scHash].SCBlockRewardMap[headerNumber.Uint64()] = currentReward
		}
	}

	for scHash := range s.SCRewardMap {
		// clear expired side chain reward record
		for number := range s.SCRewardMap[scHash].SCBlockRewardMap {
			if number < headerNumber.Uint64()-scRewardExpiredLoopCount*s.config.MaxSignerCount {
				delete(s.SCRewardMap[scHash].SCBlockRewardMap, number)
			}
		}
		// clear this side chain if reward is empty
		if len(s.SCRewardMap[scHash].SCBlockRewardMap) == 0 {
			delete(s.SCRewardMap, scHash)
		}
	}

}

func (s *Snapshot) updateSnapshotByDeclares(declares []Declare, headerNumber *big.Int) {
	for _, declare := range declares {
		if proposal, ok := s.Proposals[declare.ProposalHash]; ok {
			// check the proposal enable status and valid block number
			if proposal.ReceivedNumber.Uint64()+proposal.ValidationLoopCnt*s.config.MaxSignerCount < headerNumber.Uint64() || !s.isCandidate(declare.Declarer) {
				continue
			}
			// check if this signer already declare on this proposal
			alreadyDeclare := false
			for _, v := range proposal.Declares {
				if v.Declarer.String() == declare.Declarer.String() {
					// this declarer already declare for this proposal
					alreadyDeclare = true
					break
				}
			}
			if alreadyDeclare {
				continue
			}
			// add declare to proposal
			s.Proposals[declare.ProposalHash].Declares = append(s.Proposals[declare.ProposalHash].Declares,
				&Declare{declare.ProposalHash, declare.Declarer, declare.Decision})

		}
	}
}

func (s *Snapshot) calculateProposalResult(headerNumber *big.Int) {
	// process the expire proposal refund record
	expiredHeaderNumber := headerNumber.Uint64() - proposalRefundExpiredLoopCount*s.config.MaxSignerCount
	if _, ok := s.ProposalRefund[expiredHeaderNumber]; ok {
		delete(s.ProposalRefund, expiredHeaderNumber)
	}

	for hashKey, proposal := range s.Proposals {
		// the result will be calculate at receiverdNumber + vlcnt + 1
		if proposal.ReceivedNumber.Uint64()+proposal.ValidationLoopCnt*s.config.MaxSignerCount+1 == headerNumber.Uint64() {
			//return deposit for proposal
			if _, ok := s.ProposalRefund[headerNumber.Uint64()]; !ok {
				s.ProposalRefund[headerNumber.Uint64()] = make(map[common.Address]*big.Int)
			}
			if _, ok := s.ProposalRefund[headerNumber.Uint64()][proposal.Proposer]; !ok {
				s.ProposalRefund[headerNumber.Uint64()][proposal.Proposer] = new(big.Int).Set(proposal.CurrentDeposit)
			} else {
				s.ProposalRefund[headerNumber.Uint64()][proposal.Proposer].Add(s.ProposalRefund[headerNumber.Uint64()][proposal.Proposer], proposal.CurrentDeposit)
			}

			// calculate the current stake of this proposal
			judegmentStake := big.NewInt(0)
			for _, tally := range s.Tally {
				judegmentStake.Add(judegmentStake, tally)
			}
			judegmentStake.Mul(judegmentStake, big.NewInt(2))
			judegmentStake.Div(judegmentStake, big.NewInt(3))
			// calculate declare stake
			yesDeclareStake := big.NewInt(0)
			for _, declare := range proposal.Declares {
				if declare.Decision {
					if _, ok := s.Tally[declare.Declarer]; ok {
						yesDeclareStake.Add(yesDeclareStake, s.Tally[declare.Declarer])
					}
				}
			}
			if yesDeclareStake.Cmp(judegmentStake) > 0 {
				// process add candidate
				switch proposal.ProposalType {
				case proposalTypeCandidateAdd:
					if candidateNeedPD {
						s.Candidates[proposal.TargetAddress] = candidateStateNormal
					}
				case proposalTypeCandidateRemove:
					if _, ok := s.Candidates[proposal.TargetAddress]; ok && candidateNeedPD {
						delete(s.Candidates, proposal.TargetAddress)
					}
				case proposalTypeMinerRewardDistributionModify:
					s.MinerReward = s.Proposals[hashKey].MinerRewardPerThousand

				case proposalTypeSideChainAdd:
					if _, ok := s.SCRecordMap[proposal.SCHash]; !ok {
						s.SCRecordMap[proposal.SCHash] = &SCRecord{make(map[uint64][]*SCConfirmation), 0, 0, proposal.SCBlockCountPerPeriod, proposal.SCBlockRewardPerPeriod, make(map[common.Hash]*SCRentInfo)}
					} else {
						s.SCRecordMap[proposal.SCHash].CountPerPeriod = proposal.SCBlockCountPerPeriod
						s.SCRecordMap[proposal.SCHash].RewardPerPeriod = proposal.SCBlockRewardPerPeriod
					}
				case proposalTypeSideChainRemove:
					if _, ok := s.SCRecordMap[proposal.SCHash]; ok {
						delete(s.SCRecordMap, proposal.SCHash)
					}
				case proposalTypeMinVoterBalanceModify:
					s.MinVB = new(big.Int).Mul(new(big.Int).SetUint64(s.Proposals[hashKey].MinVoterBalance), big.NewInt(1e+18))
				case proposalTypeProposalDepositModify:
					//proposalDeposit = new(big.Int).Mul(new(big.Int).SetUint64(s.Proposals[hashKey].ProposalDeposit), big.NewInt(1e+18))
				case proposalTypeRentSideChain:
					// check if buy success
					if _, ok := s.SCRecordMap[proposal.SCHash]; !ok {
						// refund the rent fee if the side chain do not exist now, (exist when proposal)
						refundSCRentFee := new(big.Int).Mul(new(big.Int).SetUint64(s.Proposals[hashKey].SCRentFee), big.NewInt(1e+18))
						s.ProposalRefund[headerNumber.Uint64()][proposal.Proposer].Add(s.ProposalRefund[headerNumber.Uint64()][proposal.Proposer], refundSCRentFee)
					} else {
						// add rent reward info to scConfirmation
						rentFee := new(big.Int).Mul(new(big.Int).SetUint64(proposal.SCRentFee), big.NewInt(1e+18))
						rentPerPeriod := new(big.Int).Div(rentFee, new(big.Int).SetUint64(proposal.SCRentLength))
						maxRewardNumber := new(big.Int).Add(headerNumber, new(big.Int).SetUint64(proposal.SCRentLength))
						s.SCRecordMap[proposal.SCHash].RentReward[proposal.Hash] = &SCRentInfo{
							rentPerPeriod,
							maxRewardNumber,
						}
						if _, ok := s.SCNoticeMap[proposal.SCHash]; !ok {
							s.SCNoticeMap[proposal.SCHash] = &CCNotice{make(map[common.Hash]GasCharging), make(map[common.Hash]NoticeCR)}
						}
						s.SCNoticeMap[proposal.SCHash].CurrentCharging[proposal.Hash] = GasCharging{proposal.TargetAddress, proposal.SCRentFee * proposal.SCRentRate, proposal.Hash}
					}
				default:
					// todo
				}
			} else {
				// reach the target header number, but not success
				switch proposal.ProposalType {
				case proposalTypeRentSideChain:
					// refund the side chain rent fee
					refundSCRentFee := new(big.Int).Mul(new(big.Int).SetUint64(s.Proposals[hashKey].SCRentFee), big.NewInt(1e+18))
					s.ProposalRefund[headerNumber.Uint64()][proposal.Proposer].Add(s.ProposalRefund[headerNumber.Uint64()][proposal.Proposer], refundSCRentFee)
				default:
					// todo

				}
			}

			// remove all proposal
			delete(s.Proposals, hashKey)
		}

	}

}

func (s *Snapshot) updateSnapshotByProposals(proposals []Proposal, headerNumber *big.Int) {
	for _, proposal := range proposals {
		proposal.ReceivedNumber = new(big.Int).Set(headerNumber)
		s.Proposals[proposal.Hash] = &proposal
	}
}

func (s *Snapshot) updateSnapshotForExpired(headerNumber *big.Int) {

	// deal the expired vote
	var expiredVotes []*Vote
	checkBalance := false
	if len(s.Voters) > maxUncheckBalanceVoteCount {
		checkBalance = true
	}

	for voterAddress, voteNumber := range s.Voters {
		// clear the vote
		if expiredVote, ok := s.Votes[voterAddress]; ok {
			throldValue := new(big.Int).Set(s.MinVB)
			if headerNumber.Uint64() >= PosNewEffectNumber {
				throldValue = big.NewInt(0)
			}
			if headerNumber.Uint64()-voteNumber.Uint64() > s.config.Epoch || (checkBalance && s.Votes[voterAddress].Stake.Cmp(throldValue) < 0) {
				expiredVotes = append(expiredVotes, expiredVote)
			}

		}
	}
	// remove expiredVotes only enough voters left
	if uint64(len(s.Voters)-len(expiredVotes)) >= s.config.MaxSignerCount {
		for _, expiredVote := range expiredVotes {
			if _, ok := s.Tally[expiredVote.Candidate]; ok {
				s.Tally[expiredVote.Candidate].Sub(s.Tally[expiredVote.Candidate], expiredVote.Stake)
				if headerNumber.Uint64() >= PosNewEffectNumber {
					if s.Tally[expiredVote.Candidate].Cmp(big.NewInt(0)) < 0 {
						delete(s.Tally, expiredVote.Candidate)
					}
				} else {
					if s.Tally[expiredVote.Candidate].Cmp(big.NewInt(0)) == 0 {
						delete(s.Tally, expiredVote.Candidate)
					}
				}
			}
			delete(s.Votes, expiredVote.Voter)
			delete(s.Voters, expiredVote.Voter)
		}
	}

	// deal the expired confirmation
	for blockNumber := range s.Confirmations {
		if headerNumber.Uint64()-blockNumber > s.config.MaxSignerCount {
			delete(s.Confirmations, blockNumber)
		}
	}

	// remove 0 stake tally
	for address, tally := range s.Tally {
		if headerNumber.Uint64() >= PosNewEffectNumber {
			if tally.Cmp(big.NewInt(0)) < 0 {
				delete(s.Tally, address)
			}
		} else {
			if tally.Cmp(big.NewInt(0)) <= 0 {
				delete(s.Tally, address)
			}
		}

	}
}

func (s *Snapshot) updateSnapshotByConfirmations(confirmations []Confirmation) {
	for _, confirmation := range confirmations {
		_, ok := s.Confirmations[confirmation.BlockNumber.Uint64()]
		if !ok {
			s.Confirmations[confirmation.BlockNumber.Uint64()] = []*common.Address{}
		}
		addConfirmation := true
		for _, address := range s.Confirmations[confirmation.BlockNumber.Uint64()] {
			if confirmation.Signer.String() == address.String() {
				addConfirmation = false
				break
			}
		}
		if addConfirmation == true {
			var confirmSigner common.Address
			confirmSigner = confirmation.Signer
			s.Confirmations[confirmation.BlockNumber.Uint64()] = append(s.Confirmations[confirmation.BlockNumber.Uint64()], &confirmSigner)
		}
	}
}

func (s *Snapshot) updateSnapshotByVotes(votes []Vote, headerNumber *big.Int) {
	if headerNumber.Uint64() >= PosNewEffectNumber {
		s.updateSnapshotByVotesV2(votes, headerNumber)
	} else {
		for _, vote := range votes {
			// update Votes, Tally, Voters data
			if lastVote, ok := s.Votes[vote.Voter]; ok {
				s.Tally[lastVote.Candidate].Sub(s.Tally[lastVote.Candidate], lastVote.Stake)
			}
			if _, ok := s.Tally[vote.Candidate]; ok {

				s.Tally[vote.Candidate].Add(s.Tally[vote.Candidate], vote.Stake)
			} else {
				s.Tally[vote.Candidate] = new(big.Int).Set(vote.Stake)
				if !candidateNeedPD {
					s.Candidates[vote.Candidate] = candidateStateNormal
				}
			}

			s.Votes[vote.Voter] = &Vote{vote.Voter, vote.Candidate, new(big.Int).Set(vote.Stake)}
			s.Voters[vote.Voter] = headerNumber
		}
	}

}
func (s *Snapshot) updateSnapshotByVotesV2(votes []Vote, headerNumber *big.Int) {
	for _, vote := range votes {
		// update Votes, Tally, Voters data
		if lastVote, ok := s.Votes[vote.Voter]; ok {
			s.Tally[lastVote.Candidate].Sub(s.Tally[lastVote.Candidate], lastVote.Stake)
		}
		if _, ok := s.Tally[vote.Candidate]; ok {

			s.Tally[vote.Candidate].Add(s.Tally[vote.Candidate], vote.Stake)
		} else {
			s.Tally[vote.Candidate] = big.NewInt(0)
			if !candidateNeedPD {
				s.Candidates[vote.Candidate] = candidateStateNormal
			}
		}

		s.Votes[vote.Voter] = &Vote{vote.Voter, vote.Candidate, big.NewInt(0)}
		s.Voters[vote.Voter] = headerNumber
		if _, ok := s.PosPledge[vote.Candidate]; !ok {
			s.PosPledge[vote.Candidate] = &PosPledgeItem{
				Manager:     vote.Voter,
				Active:      headerNumber.Uint64(),
				TotalAmount: big.NewInt(0),
				LastPunish:  0,
				DisRate:     new(big.Int).Set(posDistributionDefaultRate),
				Detail:      make(map[common.Hash]*PledgeDetail),
			}
		}
	}
}

func (s *Snapshot) updateSnapshotByMPVotes(votes []Vote, headerNumber uint64) {
	for _, txVote := range votes {

		if lastVote, ok := s.Votes[txVote.Voter]; ok {
			s.Tally[lastVote.Candidate].Sub(s.Tally[lastVote.Candidate], lastVote.Stake)
			s.Tally[lastVote.Candidate].Add(s.Tally[lastVote.Candidate], txVote.Stake)
			s.Votes[txVote.Voter] = &Vote{Voter: txVote.Voter, Candidate: lastVote.Candidate, Stake: txVote.Stake}
			// do not modify header number of snap.Voters
			s.checkPosPledgePunish(lastVote.Candidate, headerNumber)
		}

	}
}

func (s *Snapshot) updateSnapshotForPunish(signerMissing []common.Address, headerNumber *big.Int, coinbase common.Address) {

	for _, signerEach := range signerMissing {
		if _, ok := s.Punished[signerEach]; ok {
			// 10 times of defaultFullCredit is big enough for calculate signer order
			if s.Punished[signerEach]+missingPublishCredit <= defaultFullCredit {
				s.Punished[signerEach] += missingPublishCredit
			} else {
				s.Punished[signerEach] = defaultFullCredit
			}
		} else {
			s.Punished[signerEach] = missingPublishCredit
		}
	}
	s.SignerMissing = make([]common.Address, len(signerMissing))
	copy(s.SignerMissing, signerMissing)
	// reduce the punish of sign signer
	if _, ok := s.Punished[coinbase]; ok {
		if s.Punished[coinbase] > signRewardCredit {
			s.Punished[coinbase] -= signRewardCredit
		} else {
			delete(s.Punished, coinbase)
		}
	}
	// reduce the punish for all punished
	for _, signerEach := range s.Signers {
		sigerAddr := common.HexToAddress(signerEach.String())
		if _, ok := s.Punished[sigerAddr]; ok {
			if s.Punished[sigerAddr] > autoRewardCredit {
				s.Punished[sigerAddr] -= autoRewardCredit
				s.updatePosPledgePunish(sigerAddr, headerNumber.Uint64(), headerNumber.Uint64())
			} else {
				delete(s.Punished, sigerAddr)
				s.updatePosPledgePunish(sigerAddr, 0, headerNumber.Uint64())
			}
		} else {
			s.updatePosPledgePunish(sigerAddr, 0, headerNumber.Uint64())
		}

	}
	// clear all punish score at the beginning of trantor block
	if s.config.IsTrantor(headerNumber) && !s.config.IsTrantor(new(big.Int).Sub(headerNumber, big.NewInt(1))) {
		s.Punished = make(map[common.Address]uint64)
	}

}

func isSinger(signers []*common.Address, nodeAddr string) bool {
	for signerIndex := range signers {
		if signers[signerIndex].String() == nodeAddr {
			return true
		}
	}
	return false
}

// inturn returns if a signer at a given block height is in-turn or not.
func (s *Snapshot) inturn(signer common.Address, headerTime uint64) bool {
	// if all node stop more than period of one loop
	if signersCount := len(s.Signers); signersCount > 0 {
		loopIndex := ((headerTime - s.LoopStartTime) / s.config.Period) % uint64(signersCount)
		if s.Signers[loopIndex].String() == signer.String() {
			return true
		}
	}
	return false

}

// check if side chain is exist (in side chain confirmation)
func (s *Snapshot) isSideChainExist(hash common.Hash) bool {
	if _, ok := s.SCRecordMap[hash]; ok {
		return true
	}
	return false
}

// check if address belong to voter
func (s *Snapshot) isVoter(address common.Address) bool {
	if _, ok := s.Voters[address]; ok {
		return true
	}
	return false
}

// check if address belong to candidate
func (s *Snapshot) isCandidate(address common.Address) bool {
	if _, ok := s.Candidates[address]; ok {
		return true
	}
	return false
}

// get last block number meet the confirm condition
func (s *Snapshot) getLastConfirmedBlockNumber(confirmations []Confirmation) *big.Int {

	cpyConfirmations := make(map[uint64][]*common.Address)
	for blockNumber, confirmers := range s.Confirmations {
		cpyConfirmations[blockNumber] = make([]*common.Address, len(confirmers))
		copy(cpyConfirmations[blockNumber], confirmers)
	}
	// update confirmation into snapshot
	for _, confirmation := range confirmations {
		_, ok := cpyConfirmations[confirmation.BlockNumber.Uint64()]
		if !ok {
			cpyConfirmations[confirmation.BlockNumber.Uint64()] = []*common.Address{}
		}
		addConfirmation := true
		for _, address := range cpyConfirmations[confirmation.BlockNumber.Uint64()] {
			if confirmation.Signer.String() == address.String() {
				addConfirmation = false
				break
			}
		}
		if addConfirmation == true {
			var confirmSigner common.Address
			confirmSigner = confirmation.Signer
			cpyConfirmations[confirmation.BlockNumber.Uint64()] = append(cpyConfirmations[confirmation.BlockNumber.Uint64()], &confirmSigner)
		}
	}

	i := s.Number
	for ; i > s.Number-s.config.MaxSignerCount*2/3+1; i-- {
		if confirmers, ok := cpyConfirmations[i]; ok {
			if len(confirmers) > int(s.config.MaxSignerCount*2/3) {
				return big.NewInt(int64(i))
			}
		}
	}
	return big.NewInt(int64(i))
}

func (s *Snapshot) calculateProposalRefund() map[common.Address]*big.Int {

	if refund, ok := s.ProposalRefund[s.Number-proposalRefundDelayLoopCount*s.config.MaxSignerCount]; ok {
		return refund
	}
	return make(map[common.Address]*big.Int)
}

func (s *Snapshot) calculateVoteReward(coinbase common.Address, votersReward *big.Int) (map[common.Address]*big.Int, error) {
	rewards := make(map[common.Address]*big.Int)
	allStake := big.NewInt(0)

	for voter, vote := range s.Votes {
		if vote.Candidate.String() == coinbase.String() && s.Voters[vote.Voter].Uint64() < s.Number-s.config.MaxSignerCount {
			allStake.Add(allStake, vote.Stake)
			rewards[voter] = new(big.Int).Set(vote.Stake)
		}
	}

	if allStake.Cmp(big.NewInt(0)) <= 0 && len(rewards) > 0 {
		return nil, errAllStakeMissing
	}
	for _, stake := range rewards {
		stake.Mul(stake, votersReward)
		stake.Div(stake, allStake)
	}
	return rewards, nil
}

func (s *Snapshot) calculateGasCharging() map[common.Address]*big.Int {
	gasCharge := make(map[common.Address]*big.Int)
	for hash, noticeRecord := range s.LocalNotice.ConfirmReceived {
		if noticeRecord.Success && s.Number == noticeRecord.Number+scGasChargingDelayLoopCount*s.config.MaxSignerCount {
			if charge, ok := s.LocalNotice.CurrentCharging[hash]; ok {
				if _, ok := gasCharge[charge.Target]; !ok {
					gasCharge[charge.Target] = new(big.Int).Mul(big.NewInt(1e+18), new(big.Int).SetUint64(charge.Volume))
				} else {
					gasCharge[charge.Target].Add(gasCharge[charge.Target], new(big.Int).Mul(big.NewInt(1e+18), new(big.Int).SetUint64(charge.Volume)))
				}
			}
		}
	}
	return gasCharge
}

func (s *Snapshot) calculateSCReward(minerReward *big.Int) (map[common.Address]*big.Int, *big.Int) {

	minerLeft := new(big.Int).Set(minerReward)
	scRewardAll := new(big.Int).Set(minerReward)
	scRewards := make(map[common.Address]*big.Int)

	// need to deal with sum of record.RewardPerPeriod for all side chain is larger than 100% situation
	scRewardMilliSum := uint64(0)
	for _, record := range s.SCRecordMap {
		scRewardMilliSum += record.RewardPerPeriod
	}

	if scRewardMilliSum > 0 && scRewardMilliSum < 1000 {
		scRewardAll.Mul(scRewardAll, new(big.Int).SetUint64(scRewardMilliSum))
		scRewardAll.Div(scRewardAll, big.NewInt(1000))
		minerLeft.Sub(minerLeft, scRewardAll)
		scRewardMilliSum = 1000
	} else if scRewardMilliSum >= 1000 {
		minerLeft.SetUint64(0)
	} else {
		scRewardAll.SetUint64(0)
		scRewardMilliSum = 1000
	}

	for scHash := range s.SCRewardMap {
		// check reward for the block number is exist
		if reward, ok := s.SCRewardMap[scHash].SCBlockRewardMap[s.Number-scRewardDelayLoopCount*s.config.MaxSignerCount]; ok {
			// check confirm is exist, to get countPerPeriod and rewardPerPeriod
			if confirmation, ok := s.SCRecordMap[scHash]; ok {
				// calculate the rent still not reach on this side chain
				scRentSumPerPeriod := big.NewInt(0)
				for _, rent := range confirmation.RentReward {
					if rent.MaxRewardNumber.Uint64() >= s.Number-scRewardDelayLoopCount*s.config.MaxSignerCount {
						scRentSumPerPeriod.Add(scRentSumPerPeriod, rent.RentPerPeriod)
					}
				}

				// calculate the side chain reward base on score/100 and record.RewardPerPeriod
				for addr, score := range reward.RewardScoreMap {
					singleReward := new(big.Int).Set(scRewardAll)
					singleReward.Mul(singleReward, new(big.Int).SetUint64(confirmation.RewardPerPeriod))
					singleReward.Div(singleReward, new(big.Int).SetUint64(scRewardMilliSum))
					singleReward.Add(singleReward, scRentSumPerPeriod)
					singleReward.Mul(singleReward, new(big.Int).SetUint64(score))
					singleReward.Div(singleReward, new(big.Int).SetUint64(100)) // for score/100

					if _, ok := scRewards[addr]; ok {
						scRewards[addr].Add(scRewards[addr], singleReward)
					} else {
						scRewards[addr] = singleReward
					}
				}
			}
		}
	}
	return scRewards, minerLeft
}
func (s *Snapshot) updateTallyState(state *state.StateDB) []Vote {
	var tallyVote []Vote
	for tallyAddress, vote := range s.Votes {
		amount := state.GetBalance(tallyAddress)
		if revenue, ok := s.RevenueNormal[tallyAddress]; ok {
			amount = new(big.Int).Add(amount, state.GetBalance(revenue.RevenueAddress))
		}
		tallyVote = append(tallyVote, Vote{
			Voter:     vote.Voter,
			Candidate: vote.Candidate,
			Stake:     amount,
		})

	}
	return tallyVote
}
func (s *Snapshot) updateTallyStateV2() []Vote {
	var tallyVote []Vote
	for tallyAddress, vote := range s.Votes {
		amount := big.NewInt(0)
		if item, ok := s.PosPledge[tallyAddress]; ok {
			amount = new(big.Int).Add(amount, item.TotalAmount)
		}
		tallyVote = append(tallyVote, Vote{
			Voter:     vote.Voter,
			Candidate: vote.Candidate,
			Stake:     amount,
		})

	}
	return tallyVote
}
func (s *Snapshot) updateMinerState(state *state.StateDB) []MinerStakeRecord {
	var tallyMiner []MinerStakeRecord
	for minerAddress, pledge := range s.CandidatePledge {
		if pledge.StartHigh > 0 {
			continue
		}
		if credit, ok := s.Punished[minerAddress]; ok && defaultFullCredit-minCalSignerQueueCredit >= credit {
			continue
		}
		amount := new(big.Int).Set(pledge.Amount)
		if revenue, ok := s.RevenueNormal[minerAddress]; ok {
			amount = new(big.Int).Add(amount, state.GetBalance(revenue.RevenueAddress))
		}
		amount = new(big.Int).Add(amount, state.GetBalance(minerAddress))
		if _, ok := s.TallyMiner[minerAddress]; ok {
			s.TallyMiner[minerAddress].Stake = new(big.Int).Set(amount)
		} else {
			s.TallyMiner[minerAddress] = &CandidateState{
				SignerNumber: 0,
				Stake:        new(big.Int).Set(amount),
			}
		}
		tallyMiner = append(tallyMiner, MinerStakeRecord{
			Target: minerAddress,
			Stake:  new(big.Int).Set(amount),
		})
	}
	return tallyMiner
}
func (s *Snapshot) updateMinerStateV2() []MinerStakeRecord {
	var tallyMiner []MinerStakeRecord
	for minerAddress, pledge := range s.PosPledge {
		if _, ok := s.Tally[minerAddress]; ok {
			continue
		}
		if credit, ok := s.Punished[minerAddress]; ok && defaultFullCredit-minCalSignerQueueCredit >= credit {
			continue
		}
		amount := pledge.TotalAmount
		if _, ok := s.TallyMiner[minerAddress]; ok {
			s.TallyMiner[minerAddress].Stake = new(big.Int).Set(amount)
		} else {
			s.TallyMiner[minerAddress] = &CandidateState{
				SignerNumber: 0,
				Stake:        new(big.Int).Set(amount),
			}
		}
		tallyMiner = append(tallyMiner, MinerStakeRecord{
			Target: minerAddress,
			Stake:  new(big.Int).Set(amount),
		})
	}
	return tallyMiner
}
func (s *Snapshot) updateSignerNumber(sigers []common.Address, headerNumber uint64) {

	for _, minerAddress := range sigers {
		if _, ok := s.TallyMiner[minerAddress]; ok {
			s.TallyMiner[minerAddress].SignerNumber += 1
		}
		if headerNumber >= SigerElectNewEffectBlockNumber {
			if _, ok := s.Tally[minerAddress]; ok {
				if _, isOk := s.TallySigner[minerAddress]; isOk {
					s.TallySigner[minerAddress] = s.TallySigner[minerAddress] + 1
				} else {
					s.TallySigner[minerAddress] = 1
				}
			}
		}
	}
	if headerNumber >= SigerElectNewEffectBlockNumber {
		if headerNumber%clearSignNumberPerid == 0 {
			for address, _ := range s.TallySigner {
				s.TallySigner[address] = 0
			}
			for address, _ := range s.TallyMiner {
				s.TallyMiner[address].SignerNumber = 0
			}
		}
	}
}

func (pitem *PledgeItem) copy() *PledgeItem {
	copyItem := &PledgeItem{
		Amount:          new(big.Int).Set(pitem.Amount),
		PledgeType:      pitem.PledgeType,
		Playment:        new(big.Int).Set(pitem.Playment),
		LockPeriod:      pitem.LockPeriod,
		RlsPeriod:       pitem.RlsPeriod,
		Interval:        pitem.Interval,
		StartHigh:       pitem.StartHigh,
		TargetAddress:   pitem.TargetAddress,
		RevenueAddress:  pitem.RevenueAddress,
		RevenueContract: pitem.RevenueContract,
		MultiSignature:  pitem.MultiSignature,
		BurnAddress:     pitem.BurnAddress,
	}
	if pitem.BurnRatio != nil {
		copyItem.BurnRatio = new(big.Int).Set(pitem.BurnRatio)
	}
	if pitem.BurnAmount != nil {
		copyItem.BurnAmount = new(big.Int).Set(pitem.BurnAmount)
	}
	return copyItem
}

func NewPledgeItem(amount *big.Int) *PledgeItem {
	return &PledgeItem{
		Amount:          new(big.Int).Set(amount),
		PledgeType:      0,
		Playment:        big.NewInt(0),
		LockPeriod:      0,
		RlsPeriod:       0,
		Interval:        0,
		StartHigh:       0,
		TargetAddress:   common.Address{},
		RevenueAddress:  common.Address{},
		RevenueContract: common.Address{},
		MultiSignature:  common.Address{},
		BurnAddress:     common.Address{},
		BurnRatio:       common.Big0,
		BurnAmount:      common.Big0,
	}
}

func (f *FlowMinerReport) copy() *FlowMinerReport {
	return &FlowMinerReport{
		Target:       f.Target,
		Hash:         f.Hash,
		ReportNumber: f.ReportNumber,
		FlowValue1:   f.FlowValue1,
		FlowValue2:   f.FlowValue2,
	}
}

func (snap *Snapshot) calGrantProfitHash(profit []consensus.GrantProfitRecord) common.Hash {
	grantProfitSlice := consensus.GrantProfitSlice(profit)
	sort.Sort(grantProfitSlice)
	hasher := sha3.NewLegacyKeccak256()
	rlp.Encode(hasher, grantProfitSlice)
	var hash common.Hash
	hasher.Sum(hash[:0])
	return hash
}
func (s *Snapshot) calPayProfit(db ethdb.Database, header *types.Header) ([]consensus.GrantProfitRecord, error) {
	var playGrantProfit []consensus.GrantProfitRecord
	number := header.Number.Uint64()
	if number == 0 {
		return playGrantProfit, nil
	}
	period := s.Period
	for address, item := range s.CandidatePledge {
		amount := calPaymentPledge(item, header)
		if nil != amount {
			playGrantProfit = append(playGrantProfit, consensus.GrantProfitRecord{
				Which:           sscEnumCndLock,
				MinerAddress:    address,
				BlockNumber:     0,
				Amount:          new(big.Int).Set(amount),
				RevenueAddress:  item.RevenueAddress,
				RevenueContract: item.RevenueContract,
				MultiSignature:  item.MultiSignature,
			})
		}
	}
	for address, item := range s.FlowPledge {
		amount := calPaymentPledge(item, header)
		if nil != amount {
			playGrantProfit = append(playGrantProfit, consensus.GrantProfitRecord{
				Which:           sscEnumFlwLock,
				MinerAddress:    address,
				BlockNumber:     0,
				Amount:          new(big.Int).Set(amount),
				RevenueAddress:  item.RevenueAddress,
				RevenueContract: item.RevenueContract,
				MultiSignature:  item.MultiSignature,
			})
		}
	}

	if isPaySignerRewards(number, period) {
		log.Info("LockProfitSnap cal pay reward profit")
		return s.FlowRevenue.RewardLock.calPayProfit(db, playGrantProfit, header)
	}
	if isPayFlowRewards(number, period) {
		log.Info("LockProfitSnap cal pay flow profit")
		return s.FlowRevenue.FlowLock.calPayProfit(db, playGrantProfit, header)
	}
	if isPayBandWidthRewards(number, period) {
		log.Info("LockProfitSnap cal pay bandwidth profit")
		return s.FlowRevenue.BandwidthLock.calPayProfit(db, playGrantProfit, header)
	}
	if isPayPosPledgeExit(number, period) {
		if s.FlowRevenue.PosPgExitLock != nil {
			log.Info("LockProfitSnap cal pay POS pledge exit amount")
			return s.FlowRevenue.PosPgExitLock.calPayProfit(db, playGrantProfit, header)
		}
	}
	if isPayPosExit(number, period) {
		if s.FlowRevenue.PosExitLock != nil {
			log.Info("LockProfitSnap cal pay POS exit amount")
			return s.FlowRevenue.PosExitLock.calPayProfit(db, playGrantProfit, header)
		}
	}
	if isPaySTPEntrustExit(number, period) {
		if s.FlowRevenue.STPEntrustExitLock != nil {
			log.Info("LockProfitSnap cal pay STPEntrustExit amount")
			return s.FlowRevenue.STPEntrustExitLock.calPayProfit(db, playGrantProfit, header)
		}
	}
	if isPaySTPEntrust(number, period) {
		if s.FlowRevenue.STPEntrustLock != nil {
			log.Info("LockProfitSnap cal pay STPEntrust amount")
			return s.FlowRevenue.STPEntrustLock.calPayProfit(db, playGrantProfit, header)
		}
	}
	if isPaySpReWard(number, period) {
		if s.FlowRevenue.SpLock != nil {
			log.Info("LockProfitSnap cal SP Reward amount")
			return s.FlowRevenue.SpLock.calPayProfit(db, playGrantProfit, header)
		}
	}
	if isPaySpEntrustReWard(number, period) {
		if s.FlowRevenue.SpEntrustLock != nil {
			log.Info("LockProfitSnap cal SP Entrust Reward amount")
			return s.FlowRevenue.SpEntrustLock.calPayProfit(db, playGrantProfit, header)
		}
	}
	if isPaySpExit(number, period) {
		if s.FlowRevenue.SpExitLock != nil {
			log.Info("LockProfitSnap cal SP  Exit amount")
			return s.FlowRevenue.SpExitLock.calPayProfit(db, playGrantProfit, header)
		}
	}
	if isPaySpEntrustExit(number, period) {
		if s.FlowRevenue.SpEntrustExitLock != nil {
			log.Info("LockProfitSnap cal SP Entrust Exit amount")
			return s.FlowRevenue.SpEntrustExitLock.calPayProfit(db, playGrantProfit, header)
		}
	}
	return playGrantProfit, nil
}

func (snap *Snapshot) updateGrantProfit2(grantProfitHash common.Hash, db ethdb.Database, header *types.Header) error {
	grantProfit, err := snap.calPayProfit(db, header)
	if err != nil {
		return err
	}
	calGrantProfitHash := snap.calGrantProfitHash(grantProfit)
	if grantProfitHash != calGrantProfitHash {
		log.Error("grantProfitHash is not same", "head", grantProfitHash.String(), "cal", calGrantProfitHash.String())
		return errors.New("grantProfitHash is not same,head:" + grantProfitHash.String() + "cal:" + calGrantProfitHash.String())
	}
	snap.updateGrantProfit(grantProfit, db, header.Hash(), header.Number.Uint64())
	return nil
}

func (snap *Snapshot) updateCandidatePledgeNew(candidatePledge []CandidatePledgeNewRecord, number uint64) {
	for _, item := range candidatePledge {
		if _, ok := snap.PosPledge[item.Target]; !ok {
			pledgeItem := &PosPledgeItem{
				Manager:     item.Manager,
				Active:      number,
				TotalAmount: new(big.Int).Set(item.Amount),
				Detail:      make(map[common.Hash]*PledgeDetail, 0),
				LastPunish:  uint64(0),
				DisRate:     new(big.Int).Set(posDistributionDefaultRate),
			}
			pledgeItem.Detail[item.Hash] = &PledgeDetail{
				Address: item.Manager,
				Height:  number,
				Amount:  new(big.Int).Set(item.Amount),
			}
			snap.PosPledge[item.Target] = pledgeItem
		}
		if _, ok := snap.TallyMiner[item.Target]; !ok {
			snap.TallyMiner[item.Target] = &CandidateState{
				SignerNumber: 0,
				Stake:        big.NewInt(0),
			}
		}
	}
}

func (snap *Snapshot) updateCandidatePledgeEntrust(candidatePledge []CandidatePledgeEntrustRecord, number uint64) {
	for _, item := range candidatePledge {
		if _, ok := snap.PosPledge[item.Target]; ok {
			snap.PosPledge[item.Target].Detail[item.Hash] = &PledgeDetail{
				Address: item.Address,
				Height:  number,
				Amount:  item.Amount,
			}
			snap.PosPledge[item.Target].TotalAmount = new(big.Int).Add(snap.PosPledge[item.Target].TotalAmount, item.Amount)
		}
	}
}

func (snap *Snapshot) updateCandidatePEntrustExit(candidatePledge []CandidatePEntrustExitRecord, headerNumber *big.Int) {
	for _, item := range candidatePledge {
		if _, ok := snap.PosPledge[item.Target]; ok {
			if _, ok2 := snap.PosPledge[item.Target].Detail[item.Hash]; ok2 {
				snap.PosPledge[item.Target].TotalAmount = new(big.Int).Sub(snap.PosPledge[item.Target].TotalAmount, item.Amount)
				delete(snap.PosPledge[item.Target].Detail, item.Hash)
				snap.FlowRevenue.PosExitLock.updatePosExitLockData(snap, item, headerNumber)
			}
			if !snap.isInTally(item.Target) && snap.PosPledge[item.Target].TotalAmount.Cmp(common.Big0) <= 0 {
				snap.removePosPledge(item.Target)
			}
		}
	}
}
func (snap *Snapshot) checkCandidateAutoExit(number uint64, candidateAutoExit []common.Address, state *state.StateDB, candidatePEntrustExit []CandidatePEntrustExitRecord) ([]common.Address, []CandidatePEntrustExitRecord) {
	burnAmount := common.Big0
	if isCheckPOSAutoExit(number, snap.Period) {
		for miner, item := range snap.PosPledge {
			if item.LastPunish > 0 && (number-item.LastPunish) >= maxPosContinueDayFail*snap.getBlockPreDay() {
				candidateAutoExit = append(candidateAutoExit, miner)
				for hash, detail := range snap.PosPledge[miner].Detail {
					if detail.Address == item.Manager {
						burnAmount = new(big.Int).Add(burnAmount, detail.Amount)
					} else {
						candidatePEntrustExit = append(candidatePEntrustExit, CandidatePEntrustExitRecord{
							Target:  miner,
							Hash:    hash,
							Address: detail.Address,
							Amount:  new(big.Int).Set(detail.Amount),
						})
					}
				}

			}
		}
	}
	if burnAmount.Cmp(common.Big0) > 0 {
		state.AddBalance(common.BigToAddress(big.NewInt(0)), burnAmount)
	}
	return candidateAutoExit, candidatePEntrustExit
}
func (snap *Snapshot) updateCandidateAutoExit(candidateAutoExit []common.Address, header *types.Header, db ethdb.Database) {
	if candidateAutoExit == nil || len(candidateAutoExit) == 0 {
		return
	}
	for _, miner := range candidateAutoExit {
		snap.removePosPledge(miner)
		snap.removeTally(miner)
	}
	err := snap.FlowRevenue.RewardLock.setRewardRemovePunish(candidateAutoExit, db, header.Hash(), header.Number.Uint64())
	if err != nil {
		log.Warn("setRewardRemovePunish RewardLock Error", "err", err)
	}
}

func (snap *Snapshot) isInPosCommitPeriod(minerAddress common.Address, number uint64) bool {
	if (number - snap.PosPledge[minerAddress].Active) <= (snap.SystemConfig.Deposit[sscEnumPosCommitPeriod].Uint64() * snap.getBlockPreDay()) {
		return true
	}
	return false
}

func (snap *Snapshot) isInPosCommitPeriodPass(minerAddress common.Address, number uint64, hash common.Hash, setting uint64) bool {
	pledgeDetail := snap.PosPledge[minerAddress].Detail[hash]
	if (number - pledgeDetail.Height) <= (setting * snap.getBlockPreDay()) {
		return true
	}
	return false
}

func (snap *Snapshot) findPosTargetMiner(txSender common.Address) common.Address {
	for miner, item := range snap.PosPledge {
		details := item.Detail
		for _, detail := range details {
			if detail.Address == txSender {
				return miner
			}
		}
	}
	return common.Address{}
}

func (snap *Snapshot) updateCandidateChangeRate(candidateChangeRate []CandidateChangeRateRecord, header *types.Header, db ethdb.Database) {
	for _, item := range candidateChangeRate {
		if _, ok := snap.PosPledge[item.Target]; ok {
			snap.PosPledge[item.Target].DisRate = new(big.Int).Set(item.Rate)
		}
	}
}
func (s *Snapshot) initPosPledge(number uint64) {
	for addr, _ := range s.Tally {
		if _, ok := s.PosPledge[addr]; !ok {
			lastPunish := uint64(0)
			if punishVal, ok1 := s.Punished[addr]; ok1 {
				lastPunish = punishVal
			}
			managerAddr := addr
			if revenue, ok1 := s.RevenueNormal[addr]; ok1 {
				managerAddr = revenue.RevenueAddress
			}

			s.PosPledge[addr] = &PosPledgeItem{
				Manager:     managerAddr,
				Active:      number,
				TotalAmount: big.NewInt(0),
				LastPunish:  lastPunish,
				DisRate:     new(big.Int).Set(posDistributionDefaultRate),
				Detail:      make(map[common.Hash]*PledgeDetail),
			}
			if pledgeItem, ok1 := s.CandidatePledge[addr]; ok1 && pledgeItem.StartHigh <= 0 {
				s.PosPledge[addr].TotalAmount = pledgeItem.Amount
				s.PosPledge[addr].Detail[common.HexToHash(managerAddr.String())] = &PledgeDetail{
					Address: managerAddr,
					Height:  number,
					Amount:  pledgeItem.Amount,
				}
			}
			if _, ok2 := s.TallyMiner[addr]; ok2 {
				delete(s.TallyMiner, addr)
			}
		}
	}
	for addr, _ := range s.TallyMiner {
		lastPunish := uint64(0)
		if punishVal, ok1 := s.Punished[addr]; ok1 {
			lastPunish = punishVal
		}
		managerAddr := addr
		if revenue, ok1 := s.RevenueNormal[addr]; ok1 {
			managerAddr = revenue.RevenueAddress
		}
		s.PosPledge[addr] = &PosPledgeItem{
			Manager:     managerAddr,
			Active:      number,
			TotalAmount: big.NewInt(0),
			LastPunish:  lastPunish,
			DisRate:     new(big.Int).Set(posDistributionDefaultRate),
			Detail:      make(map[common.Hash]*PledgeDetail),
		}
		if pledgeItem, ok1 := s.CandidatePledge[addr]; ok1 && pledgeItem.StartHigh <= 0 {
			s.PosPledge[addr].TotalAmount = pledgeItem.Amount
			s.PosPledge[addr].Detail[common.HexToHash(managerAddr.String())] = &PledgeDetail{
				Address: managerAddr,
				Height:  number,
				Amount:  pledgeItem.Amount,
			}
		}
	}

}

func (s *Snapshot) updatePosPledgePunish(address common.Address, punishNumber uint64, headerNumber uint64) {
	if headerNumber > PosNewEffectNumber {
		if item, ok := s.PosPledge[address]; ok {
			if punishNumber == 0 && item.LastPunish > 0 {
				item.LastPunish = 0
			}
			if punishNumber > 0 && item.LastPunish == 0 {
				item.LastPunish = punishNumber
			}

		}
	}
}
func (s *Snapshot) checkPosPledgePunish(address common.Address, headerNumber uint64) {
	if headerNumber > PosNewEffectNumber {
		if pledge, ok1 := s.PosPledge[address]; ok1 {
			if _, ok2 := s.Punished[address]; ok2 {
				if pledge.LastPunish == 0 {
					pledge.LastPunish = headerNumber
				}
			} else {
				if pledge.LastPunish > 0 {
					pledge.LastPunish = 0
				}
			}
		}
	}
}
func (s *Snapshot) updateTallyMiner(addr common.Address) {

	if item, ok1 := s.PosPledge[addr]; ok1 {
		if _, ok := s.Tally[addr]; ok {
			s.Tally[addr] = item.TotalAmount
		}
		if _, ok := s.TallyMiner[addr]; ok {
			s.TallyMiner[addr].Stake = item.TotalAmount
		}

	}
}

func (s *Snapshot) fixStorageRevertRevenue(header *types.Header, db ethdb.Database) {
	err := s.FlowRevenue.PosPgExitLock.fixStorageRevertRevenue(db, header.Hash(), header.Number.Uint64())
	if err != nil {
		log.Warn("fixStorageRevertRevenue Error", "err", err)
	}
}

func (snap *Snapshot) removePosPledge(miner common.Address) {
	if _, ok := snap.PosPledge[miner]; ok {
		delete(snap.PosPledge, miner)
	}
	if _, ok := snap.RevenueNormal[miner]; ok {
		delete(snap.RevenueNormal, miner)
	}
	if _, ok := snap.TallyMiner[miner]; ok {
		delete(snap.TallyMiner, miner)
	}
}

func (s *Snapshot) deletePunishByPosExit(headerNumber uint64) {
	if headerNumber >= PosNewEffectNumber {
		for punishAddr, _ := range s.Punished {
			if _, ok := s.PosPledge[punishAddr]; !ok {
				delete(s.Punished, punishAddr)
			}
		}
	}
}

func (s *Snapshot) isInTally(minerAddress common.Address) bool {
	if _, ok := s.Tally[minerAddress]; ok {
		return true
	}
	return false
}

func (snap *Snapshot) removeTally(miner common.Address) {
	if _, ok := snap.Tally[miner]; ok {
		delete(snap.Tally, miner)
	}
	if _, ok := snap.Votes[miner]; ok {
		delete(snap.Votes, miner)
	}
	if _, ok := snap.Voters[miner]; ok {
		delete(snap.Voters, miner)
	}
	if _, ok := snap.Candidates[miner]; ok {
		delete(snap.Candidates, miner)
	}
}

func (snap *Snapshot) initPosExitPunish(header *types.Header, db ethdb.Database) {
	delAddress := make([]common.Address, 0)
	exitAddress := make([]common.Address, 0)
	for address, item := range snap.CandidatePledge {
		if item.StartHigh > 0 {
			item.BurnRatio = new(big.Int).Set(BurnBase)
			item.BurnAmount = common.Big0
			exitAddress = append(exitAddress, address)
		} else {
			delAddress = append(delAddress, address)
		}
	}
	err := snap.FlowRevenue.RewardLock.setRewardRemovePunish(exitAddress, db, header.Hash(), header.Number.Uint64())
	if err != nil {
		log.Warn("setRewardRemovePunish RewardLock Error", "err", err)
	}
	for _, address := range delAddress {
		delete(snap.CandidatePledge, address)
	}
}

func (snap *Snapshot) isSystemManager(txSender common.Address) bool {
	return snap.SystemConfig.ManagerAddress[sscEnumSystem] == txSender
}

func (snap *Snapshot) isSystemManagerAndInTally(txSender common.Address, minerAddress common.Address) bool {
	return snap.isSystemManager(txSender) && snap.isInTally(minerAddress)
}

func (snap *Snapshot) updateCandidateExit2(candidateExit []common.Address, number *big.Int) {
	if candidateExit == nil || len(candidateExit) == 0 {
		return
	}
	for _, miner := range candidateExit {
		snap.removePosPledge(miner)
		snap.removeTally(miner)
	}
}

func (snap *Snapshot) isPosMinerManager(target common.Address) bool {
	for _, item := range snap.PosPledge {
		if item.Manager == target {
			return true
		}
	}
	return false
}

func (snap *Snapshot) initPosExitPunishFix() {
	for _, item := range snap.PosPledge {
		if item.LastPunish > 0 && item.LastPunish < (PosNewEffectNumber-1) {
			item.LastPunish = PosNewEffectNumber - 1
		}
	}
}

func (snap *Snapshot) updateTotalLeaseSpace(curLeaseSpace *big.Int) {
	if curLeaseSpace != nil && 0 < curLeaseSpace.Cmp(big.NewInt(0)) {
		if nil == snap.TotalLeaseSpace {
			snap.TotalLeaseSpace = new(big.Int).Set(curLeaseSpace)
		} else {
			snap.TotalLeaseSpace = new(big.Int).Add(snap.TotalLeaseSpace, curLeaseSpace)
		}
	}
}

func (s *Snapshot) updatePOSTransfer(posTransferRecord []POSTransferRecord, number *big.Int) {
	if len(posTransferRecord) == 0 {
		return
	}
	spCount := 0
	snCount := 0

	for _, record := range posTransferRecord {
		if TargetTypePos == record.TargetType {
			if posItem, ok := s.PosPledge[record.Target]; ok {
				posItem.Detail[record.PledgeHash] = &PledgeDetail{
					Address: record.Address,
					Height:  number.Uint64(),
					Amount:  record.PledgeAmount,
				}
				posItem.TotalAmount = new(big.Int).Add(posItem.TotalAmount, record.PledgeAmount)
			}
		} else if TargetTypeSp == record.TargetType {
			if targetSp, ok := s.SpData.PoolPledge[record.TargetHash]; ok {
				targetSp.TotalAmount = new(big.Int).Add(targetSp.TotalAmount, record.PledgeAmount)
				preCapacity := getCapacity(targetSp.TotalAmount)
				if preCapacity.Cmp(targetSp.TotalCapacity) > 0 {
					targetSp.TotalCapacity = preCapacity
				}
				if targetSp.Manager==record.Address{
					targetSp.ManagerAmount=new(big.Int).Add(targetSp.ManagerAmount,record.PledgeAmount)
				}
				targetSp.EtDetail[record.PledgeHash] = &EntrustDetail{
					Address: record.Address,
					Height:  new(big.Int).Set(number),
					Amount:  record.PledgeAmount,
					Hash:    getHash(changeOxToUx(record.Address.String()) + record.PledgeAmount.String() + number.String()),
				}
				s.SpData.accumulateSpPledgelHash(record.TargetHash, false)
				spCount++
			}
		} else if TargetTypeSn == record.TargetType {
			if targetSn, ok := s.StorageData.StorageEntrust[record.Target]; ok {
				targetSn.PledgeAmount = new(big.Int).Add(targetSn.PledgeAmount, record.PledgeAmount)
				targetSn.Detail[record.PledgeHash] = &SEntrustDetail{
					Address: record.Address,
					Height:  new(big.Int).Set(number),
					Amount:  record.PledgeAmount,
				}
				if record.Address==targetSn.Manager{
					targetSn.ManagerAmount=new(big.Int).Add(targetSn.ManagerAmount,record.PledgeAmount)
					targetSn.Managerheight=new(big.Int).Set(number)
				}
				if stp, ok1 := s.StorageData.StoragePledge[record.Target]; ok1 {
					if targetSn.PledgeAmount.Cmp(stp.SpaceDeposit)>=0&&stp.PledgeStatus.Cmp(big.NewInt(SPledgeInactive))==0{
						s.StorageData.StoragePledge[record.Target].PledgeStatus=big.NewInt(SPledgeNormal)
						s.StorageData.accumulatePledgeHash(record.Target)
					}
				}
			}
			snCount++
		}
		if se, ok := s.PosPledge[record.Original]; ok {
			delHash := make([]common.Hash, 0)
			for etHash, detail := range se.Detail {
				if record.Address == detail.Address {
					delHash = append(delHash, etHash)
				}
			}
			for _, removeHash := range delHash {
				delete(se.Detail, removeHash)
			}
			se.TotalAmount = new(big.Int).Sub(se.TotalAmount, new(big.Int).Add(record.LockAmount, record.PledgeAmount))
			if record.LockAmount.Cmp(common.Big0)>0{
				s.FlowRevenue.PosExitLock.updatePosTransferLockData(s, record, number)
			}
		}
		if spCount > 0 {
			s.SpData.accumulateSpDataHash()
		}
		if snCount > 0 {
			
		}

	}
}

func (snap *Snapshot) findSPTargetMiner(txSender common.Address) common.Hash {
	for pool, item := range snap.SpData.PoolPledge {
		details := item.EtDetail
		for _, detail := range details {
			if detail.Address == txSender {
				return pool
			}
		}
	}
	return common.Hash{}
}

