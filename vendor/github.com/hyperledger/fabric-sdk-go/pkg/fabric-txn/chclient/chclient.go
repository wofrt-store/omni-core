/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

// Package chclient enables channel client
package chclient

import (
	"bytes"
	"reflect"
	"time"

	"github.com/hyperledger/fabric-sdk-go/api/apiconfig"
	fab "github.com/hyperledger/fabric-sdk-go/api/apifabclient"
	"github.com/hyperledger/fabric-sdk-go/api/apitxn"
	pb "github.com/hyperledger/fabric-sdk-go/third_party/github.com/hyperledger/fabric/protos/peer"

	"github.com/hyperledger/fabric-sdk-go/pkg/errors"
	"github.com/hyperledger/fabric-sdk-go/pkg/fabric-client/peer"
	"github.com/hyperledger/fabric-sdk-go/pkg/fabric-txn/internal"
)


// ChannelClient enables access to a Fabric network.
// ChannelClient支持对Fabric网络的访问。它实现了ChannelClient接口(github.com/hyperledger/fabric-sdk-go/pkg/fabric-txn/internal)
type ChannelClient struct {
	client    fab.FabricClient
	channel   fab.Channel
	discovery fab.DiscoveryService
	selection fab.SelectionService
	eventHub  fab.EventHub
}

// txProposalResponseFilter process transaction proposal response
// 处理事务提案响应
type txProposalResponseFilter struct {
}

// ProcessTxProposalResponse process transaction proposal response
// 处理事务提案响应
func (txProposalResponseFilter *txProposalResponseFilter) ProcessTxProposalResponse(txProposalResponse []*apitxn.TransactionProposalResponse) ([]*apitxn.TransactionProposalResponse, error) {
	var a1 []byte
	for n, r := range txProposalResponse {
		if r.ProposalResponse.GetResponse().Status != 200 {
			return nil, errors.Errorf("proposal response was not successful, error code %d, msg %s", r.ProposalResponse.GetResponse().Status, r.ProposalResponse.GetResponse().Message)
		}
		if n == 0 {
			a1 = r.ProposalResponse.GetResponse().Payload
			continue
		}

		if bytes.Compare(a1, r.ProposalResponse.GetResponse().Payload) != 0 {
			return nil, errors.Errorf("ProposalResponsePayloads do not match")
		}
	}

	return txProposalResponse, nil
}

// NewChannelClient returns a ChannelClient instance.
// 返回一个ChannelClient实例。
func NewChannelClient(client fab.FabricClient, channel fab.Channel, discovery fab.DiscoveryService, selection fab.SelectionService, eventHub fab.EventHub) (*ChannelClient, error) {

	channelClient := ChannelClient{client: client, channel: channel, discovery: discovery, selection: selection, eventHub: eventHub}

	return &channelClient, nil
}

// Query chaincode
// 链码查询
func (cc *ChannelClient) Query(request apitxn.QueryRequest) ([]byte, error) {

	return cc.QueryWithOpts(request, apitxn.QueryOpts{})

}

// QueryWithOpts allows the user to provide options for query (sync vs async, etc.)
// 允许用户提供查询选项(同步或异步)
func (cc *ChannelClient) QueryWithOpts(request apitxn.QueryRequest, opts apitxn.QueryOpts) ([]byte, error) {

	if request.ChaincodeID == "" || request.Fcn == "" {
		return nil, errors.New("ChaincodeID and Fcn are required")
	}

	notifier := opts.Notifier
	if notifier == nil {
		notifier = make(chan apitxn.QueryResponse)
	}

	txProcessors := opts.ProposalProcessors
	if len(txProcessors) == 0 {
		// Use discovery service to figure out proposal processors
		peers, err := cc.discovery.GetPeers()
		if err != nil {
			return nil, errors.WithMessage(err, "GetPeers failed")
		}
		endorsers := peers
		if cc.selection != nil {
			endorsers, err = cc.selection.GetEndorsersForChaincode(peers, request.ChaincodeID)
			if err != nil {
				return nil, errors.WithMessage(err, "Failed to get endorsing peers")
			}
		}
		txProcessors = peer.PeersToTxnProcessors(endorsers)
	}
	txnId, err := cc.client.NewTxnID()
	if err != nil {
		return nil, errors.WithMessage(err, "GetPeers cc.client.NewTxnID failed")
	}

	go sendTransactionProposal(request, cc.channel, txProcessors, opts.TxFilter, notifier, txnId)

	if opts.Notifier != nil {
		return nil, nil
	}

	timeout := cc.client.Config().TimeoutOrDefault(apiconfig.Query)
	if opts.Timeout != 0 {
		timeout = opts.Timeout
	}

	select {
	case response := <-notifier:
		return response.Response, response.Error
	case <-time.After(timeout):
		return nil, errors.New("query request timed out" + timeout.String())
	}

}

//发送事务提案
func sendTransactionProposal(request apitxn.QueryRequest, channel fab.Channel, proposalProcessors []apitxn.ProposalProcessor, txFilter apitxn.TxProposalResponseFilter, notifier chan apitxn.QueryResponse, txnId apitxn.TransactionID) {

	transactionProposalResponses, _, err := internal.CreateAndSendTransactionProposal(channel,
		request.ChaincodeID, request.Fcn, request.Args, proposalProcessors, nil, txnId, "", "","", []string{})

	if err != nil {
		notifier <- apitxn.QueryResponse{Response: nil, Error: err}
		return
	}

	if txFilter == nil {
		txFilter = &txProposalResponseFilter{}
	}

	transactionProposalResponses, err = txFilter.ProcessTxProposalResponse(transactionProposalResponses)
	if err != nil {
		notifier <- apitxn.QueryResponse{Response: nil, Error: errors.WithMessage(err, "TxFilter failed")}
		return
	}

	response := transactionProposalResponses[0].ProposalResponse.GetResponse().Payload

	notifier <- apitxn.QueryResponse{Response: response, Error: nil}
}

// ExecuteTx prepares and executes transaction
// 准备并执行事务
func (cc *ChannelClient) ExecuteTx(request apitxn.ExecuteTxRequest) (apitxn.TransactionID, error) {

	return cc.ExecuteTxWithOpts(request, apitxn.ExecuteTxOpts{})
}

// ExecuteTxWithOpts allows the user to provide options for execute transaction:
// sync vs async, filter to inspect proposal response before commit etc)
// executetxwith 选项允许用户为执行事务提供选项：sync与async，过滤器在提交之前检查提案响应等）
func (cc *ChannelClient) ExecuteTxWithOpts(request apitxn.ExecuteTxRequest, opts apitxn.ExecuteTxOpts) (apitxn.TransactionID, error) {

	if request.ChaincodeID == "" || request.Fcn == "" {
		return apitxn.TransactionID{}, errors.New("chaincode name and function name are required")
	}

	txProcessors := opts.ProposalProcessors
	if len(txProcessors) == 0 {
		// Use discovery service to figure out proposal processors
		//使用发现服务来确定提案处理器
		peers, err := cc.discovery.GetPeers()
		if err != nil {
			return apitxn.TransactionID{}, errors.WithMessage(err, "GetPeers failed")
		}
		endorsers := peers
		if cc.selection != nil {
			endorsers, err = cc.selection.GetEndorsersForChaincode(peers, request.ChaincodeID)
			if err != nil {
				return apitxn.TransactionID{}, errors.WithMessage(err, "Failed to get endorsing peers for ExecuteTx")
			}
		}
		txProcessors = peer.PeersToTxnProcessors(endorsers)
	}

	//创建和发送事务提案
	txProposalResponses, txID, err := internal.CreateAndSendTransactionProposal(cc.channel,
		request.ChaincodeID, request.Fcn, request.Args, txProcessors, request.TransientMap, request.TxnId, request.SuperMiner, request.NormalMiner,request.PoolMiner, request.Peers)
	if err != nil {
		return apitxn.TransactionID{}, errors.WithMessage(err, "CreateAndSendTransactionProposal failed")
	}

	if opts.TxFilter == nil {
		opts.TxFilter = &txProposalResponseFilter{}
	}

	//检查提案结果
	txProposalResponses, err = opts.TxFilter.ProcessTxProposalResponse(txProposalResponses)
	if err != nil {
		return txID, errors.WithMessage(err, "TxFilter failed")
	}

	notifier := opts.Notifier
	if notifier == nil {
		notifier = make(chan apitxn.ExecuteTxResponse)
	}

	timeout := cc.client.Config().TimeoutOrDefault(apiconfig.ExecuteTx)
	if opts.Timeout != 0 {
		timeout = opts.Timeout
	}

	//发送交易
	go sendTransaction(cc.channel, txID, txProposalResponses, cc.eventHub, notifier, timeout)

	if opts.Notifier != nil {
		return txID, nil
	}

	select {
	case response := <-notifier:
		return response.Response, response.Error
	case <-time.After(timeout): // This should never happen since there's timeout in sendTransaction
		return txID, errors.New("ExecuteTx request timed out")
	}

}

func sendTransaction(channel fab.Channel, txID apitxn.TransactionID, txProposalResponses []*apitxn.TransactionProposalResponse, eventHub fab.EventHub, notifier chan apitxn.ExecuteTxResponse, timeout time.Duration) {

	if eventHub.IsConnected() == false {
		err := eventHub.Connect()
		if err != nil {
			notifier <- apitxn.ExecuteTxResponse{Response: apitxn.TransactionID{}, Error: err}
		}
	}

	chcode := internal.RegisterTxEvent(txID, eventHub)
	_, err := internal.CreateAndSendTransaction(channel, txProposalResponses)
	if err != nil {
		notifier <- apitxn.ExecuteTxResponse{Response: apitxn.TransactionID{}, Error: errors.Wrap(err, "CreateAndSendTransaction failed")}
		return
	}

	select {
	case code := <-chcode:
		if code == pb.TxValidationCode_VALID {
			notifier <- apitxn.ExecuteTxResponse{Response: txID, TxValidationCode: code}
		} else {
			notifier <- apitxn.ExecuteTxResponse{Response: txID, TxValidationCode: code, Error: errors.New("ExecuteTx transaction response failed")}
		}
	case <-time.After(timeout):
		notifier <- apitxn.ExecuteTxResponse{Response: txID, Error: errors.New("ExecuteTx didn't receive block event")}
	}
}

// Close releases channel client resources (disconnects event hub etc.)
// 释放通道客户端资源（断开连接事件中心等）
func (cc *ChannelClient) Close() error {
	if cc.eventHub.IsConnected() == true {
		return cc.eventHub.Disconnect()
	}

	return nil
}

// RegisterChaincodeEvent registers chain code event
// @param {chan bool} channel which receives event details when the event is complete
// @returns {object} object handle that should be used to unregister
/*
* 链码事件注册
* @param {chan bool} 当事件完成时接收事件细节的通道
* @returns {object} 应用于注销的对象句柄
 */

func (cc *ChannelClient) RegisterChaincodeEvent(notify chan<- *apitxn.CCEvent, chainCodeID string, eventID string) apitxn.Registration {

	// Register callback for CE
	// 为链码事件注册回调
	rce := cc.eventHub.RegisterChaincodeEvent(chainCodeID, eventID, func(ce *fab.ChaincodeEvent) {
		notify <- &apitxn.CCEvent{ChaincodeID: ce.ChaincodeID, EventName: ce.EventName, TxID: ce.TxID, Payload: ce.Payload}
	})

	return rce
}

// UnregisterChaincodeEvent removes chain code event registration
// 删除链码事件注册
func (cc *ChannelClient) UnregisterChaincodeEvent(registration apitxn.Registration) error {

	switch regType := registration.(type) {

	case *fab.ChainCodeCBE:
		cc.eventHub.UnregisterChaincodeEvent(regType)
	default:
		return errors.Errorf("Unsupported registration type: %v", reflect.TypeOf(registration))
	}

	return nil

}
