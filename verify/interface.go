package verifier

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/election"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/params"
)

func newPayLoadInfo(info *election.NodeInfo, electType uint32) *types.ElectionTxPayLoadInfo {
	return &types.ElectionTxPayLoadInfo{
		TPS:        info.TPS,
		IP:         info.IP,
		ID:         info.ID,
		Wealth:     info.Wealth,
		OnlineTime: info.OnlineTime,
		TxHash:     info.TxHash,
		Value:      info.Value,
		ElectType:  electType,
		Account:    info.Account,
	}
}

func mergeNodeInfo(nodeMap map[string]*types.ElectionTxPayLoadInfo, nodeList []election.NodeInfo, electType uint32) {
	if nil == nodeList {
		return
	}

	for _, nodeInfo := range nodeList {
		if _, ok := nodeMap[nodeInfo.ID]; ok {
			// 已存在，说明节点有新的参选退选操作，以新操作为准
			continue
		} else {
			nodeMap[nodeInfo.ID] = newPayLoadInfo(&nodeInfo, electType)
		}
	}
}

func (v *Verifier) GenerateMainNodeList(currentNumber uint64) (election.NodeList, error) {
	var returnList election.NodeList

	if (currentNumber+2)%params.BroadcastInterval != 0 {
		return returnList, fmt.Errorf("block number = %d, it is not the time to generate main node list", currentNumber)
	}

	var minerList, committeeList, bothList []election.NodeInfo
	lastBroadcastBlkNumber := currentNumber + 2 - params.BroadcastInterval
	if lastBroadcastBlkNumber == 0 {
		minerList = nil
		committeeList = nil
		bothList = nil

		// 使用boot节点作为0广播区块的主节点列表
		committeeList = make([]election.NodeInfo, 0)
		if committeeNode, err := discover.ParseNode(params.MainnetBootnodes[0]); err == nil {
			committeeList = append(committeeList, election.NodeInfo{ID: committeeNode.ID.String(), IP: committeeNode.IP.String(), Wealth: 10000})
		}

		minerList = make([]election.NodeInfo, 0)
		for i := 1; i < len(params.MainnetBootnodes); i++ {
			if bootNode, err := discover.ParseNode(params.MainnetBootnodes[i]); err == nil {
				minerList = append(minerList, election.NodeInfo{ID: bootNode.ID.String(), IP: bootNode.IP.String(), Wealth: 10000})
			}
		}

	} else {
		lastBroadcastBlk := v.chain.GetBlockByNumber(lastBroadcastBlkNumber)
		if nil == lastBroadcastBlk {
			return returnList, fmt.Errorf("get last broadcast block(%d) err", lastBroadcastBlkNumber)
		}
		minerList = lastBroadcastBlk.Header().MinerList
		committeeList = lastBroadcastBlk.Header().CommitteeList
		bothList = lastBroadcastBlk.Header().Both
	}

	var startPos uint64
	if currentNumber > params.BroadcastInterval+1 {
		startPos = currentNumber - params.BroadcastInterval - 1
	} else {
		startPos = 0
	}

	newNodeMap, err := v.GetElectionAndExitNodeInfo(startPos, currentNumber)
	if err != nil {
		return returnList, err
	}
	// 将上个广播区块中的主节点列表和本周期内出现的新参选退选合并，其中上个广播区块中退选列表不需要合并
	mergeNodeInfo(newNodeMap, minerList, types.ElectMiner)
	mergeNodeInfo(newNodeMap, committeeList, types.ElectCommittee)
	mergeNodeInfo(newNodeMap, bothList, types.ElectBoth)

	// 由合并后的map生产本周期的主节点列表
	for _, value := range newNodeMap {
		info := election.NodeInfo{
			TPS:        value.TPS,
			IP:         value.IP,
			ID:         value.ID,
			Wealth:     value.Wealth,
			OnlineTime: value.OnlineTime,
			TxHash:     value.TxHash,
			Value:      value.Value,
			Account:    value.Account,
		}

		switch value.ElectType {
		case types.ElectExit:
			returnList.OfflineList = append(returnList.OfflineList, info)
		case types.ElectMiner:
			returnList.MinerList = append(returnList.MinerList, info)
		case types.ElectCommittee:
			returnList.CommitteeList = append(returnList.CommitteeList, info)
		case types.ElectBoth:
			returnList.Both = append(returnList.Both, info)
		}
	}

	return returnList, nil
}

func (v *Verifier) GetElectionAndExitNodeInfo(blkNumStart uint64, blkNumEnd uint64) (map[string]*types.ElectionTxPayLoadInfo, error) {
	if blkNumStart > blkNumEnd {
		return nil, fmt.Errorf("input block number index err")
	}

	infoMap := make(map[string]*types.ElectionTxPayLoadInfo)

	//todo 可能需要添加，一个节点出现多次交易时，累加交易金额。考虑中间出现退出时，清空的情况
	pos := blkNumStart
	for ; pos <= blkNumEnd; pos++ {
		block := v.chain.GetBlockByNumber(pos)
		if nil == block {
			continue
		}

		for _, tx := range block.Transactions() {
			if nil == tx {
				continue
			}

			nodeInfo := tx.ParseElectionTxPayLoad()
			if nodeInfo == nil {
				continue
			}

			//get from account address
			config := v.chain.Config()
			height := v.chain.CurrentBlock().Header().Number
			fromAddress, err := types.Sender(types.MakeSigner(config, height), tx)
			if err != nil {
				log.Warn("Get from account address err!")
				continue
			}
			nodeInfo.Account = fromAddress

			infoMap[nodeInfo.ID] = nodeInfo
		}
	}

	return infoMap, nil
}