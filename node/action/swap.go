/*
	Copyright 2017-2018 OneLedger

	An incoming transaction, send, swap, ready, verification, etc.
*/
package action

import (
	"bytes"
	"strings"

	"github.com/Oneledger/protocol/node/chains/bitcoin"
	"github.com/Oneledger/protocol/node/comm"
	"github.com/Oneledger/protocol/node/convert"
	"github.com/Oneledger/protocol/node/data"
	"github.com/Oneledger/protocol/node/err"
	"github.com/Oneledger/protocol/node/global"
	"github.com/Oneledger/protocol/node/id"
	"github.com/Oneledger/protocol/node/log"
)

// Synchronize a swap between two users
type Swap struct {
	Base

	Party        id.AccountKey `json:"party"`
	CounterParty id.AccountKey `json:"counter_party"`
	Amount       data.Coin     `json:"amount"`
	Exchange     data.Coin     `json:"exchange"`
	Fee          data.Coin     `json:"fee"`
	Gas          data.Coin     `json:"fee"`
	Nonce        int64         `json:"nonce"`
	Preimage     []byte        `json:"preimage"`
}

// Ensure that all of the base values are at least reasonable.
func (transaction *Swap) Validate() err.Code {
	log.Debug("Validating Swap Transaction")

	if transaction.Party == nil {
		return err.MISSING_DATA
	}
	if transaction.CounterParty == nil {
		return err.MISSING_DATA
	}
	if !transaction.Amount.IsValid() {
		return err.MISSING_DATA
	}
	return err.SUCCESS
}

func (transaction *Swap) ProcessCheck(app interface{}) err.Code {
	log.Debug("Processing Swap Transaction for CheckTx")

	// TODO: Check all of the data to make sure it is valid.

	return err.SUCCESS
}

// Start the swap
func (transaction *Swap) ProcessDeliver(app interface{}) err.Code {
	log.Debug("Processing Swap Transaction for DeliverTx")

	if ProcessSwap(app, transaction) {
		commands := transaction.Expand(app)

		Resolve(app, transaction, commands)

		for i := 0; i < commands.Count(); i++ {
			status := Execute(app, commands[i])
			if status != err.SUCCESS {
				log.Error("Failed to Execute", "command", commands[i])
				return err.EXPAND_ERROR
			}
		}
	}

	return err.SUCCESS
}

func FindSwap(status *data.Datastore, key id.AccountKey) Transaction {
	result := status.Load(key)
	var transaction Transaction
	buffer, err := comm.Deserialize(result, transaction)
	if err != nil {
		return nil
	}
	return buffer.(Transaction)
}

// TODO: Change to return Role as INITIATOR or PARTICIPANT
func FindMatchingSwap(status *data.Datastore, counterParty id.Account, role Role, transaction *Swap) bool {

	result := FindSwap(status, counterParty.AccountKey())
	if result != nil {
		if MatchSwap(result.(*Swap), transaction) {
			return true
		}
	}

	return false
}

func MatchSwap(left *Swap, right *Swap) bool {
	if left.Base.Type != right.Base.Type {
		return false
	}
	if left.Base.Sequence != right.Base.Sequence {
		return false
	}
	if bytes.Compare(left.Party, right.Party) == 0 {
		return false
	}
	if bytes.Compare(left.CounterParty, right.CounterParty) == 0 {
		return false
	}
	if left.Amount != right.Amount {
		return false
	}
	return true
}

func ProcessSwap(app interface{}, transaction *Swap) bool {
	status := GetStatus(app)

	account := transaction.GetNodeAccount(app)
	role := transaction.GetRole(account)

	if role == NONE {
		log.Error("Can't find a role for this swap")
		return false
	}

	var primary id.Account
	if role == INITIATOR {
		primary = GetAccount(app, transaction.Party)
	} else {
		primary = GetAccount(app, transaction.CounterParty)
	}

	SaveSwap(status, primary, transaction)

	if FindMatchingSwap(status, primary, role, transaction) {
		return true
	}
	return false
}

func SaveSwap(status *data.Datastore, account id.Account, transaction *Swap) {
	buffer, _ := comm.Serialize(transaction)

	status.Store(account.AccountKey(), buffer)
}

// Is this node one of the partipants in the swap
func (transaction *Swap) ShouldProcess(app interface{}) bool {
	account := transaction.GetNodeAccount(app)

	if transaction.GetRole(account) != ALL {
		return true
	}

	return false
}

func GetAccount(app interface{}, accountKey id.AccountKey) id.Account {
	accounts := GetAccounts(app)
	account, _ := accounts.FindKey(accountKey)

	return account
}

// Map the identity to a specific account on a chain
func GetChainAccount(app interface{}, name string, chain data.ChainType) id.Account {
	identities := GetIdentities(app)
	accounts := GetAccounts(app)

	identity, _ := identities.FindName(name)
	account, _ := accounts.FindKey(identity.Chain[chain])

	return account
}

func (transaction *Swap) GetNodeAccount(app interface{}) id.Account {

	identities := GetIdentities(app)
	if identities == nil {
		log.Error("Indentities database missing")
		return nil
	}

	identity, _ := identities.FindName(global.Current.NodeAccountName)
	if identity == nil {
		log.Error("Node does not have name or not registered", "name", global.Current.NodeAccountName)
		return nil
	}

	accounts := GetAccounts(app)
	if identities == nil {
		log.Error("Accounts database missing")
		return nil
	}

	account, _ := accounts.FindIdentity(*identity)
	if identity == nil {
		log.Error("Node does not have account")
		return nil
	}

	return account
}

func (transaction *Swap) GetRole(account id.Account) Role {
	if account == nil {
		return NONE
	}

	initiator := transaction.Party
	participant := transaction.CounterParty

	if bytes.Compare(initiator, account.AccountKey()) == 0 {
		return INITIATOR
	}

	if bytes.Compare(participant, account.AccountKey()) == 0 {
		return PARTICIPANT
	}

	// TODO: Shouldn't be in-band this way
	return ALL
}

// Given a transaction, expand it into a list of Commands to execute against various chains.
func (transaction *Swap) Expand(app interface{}) Commands {
	chains := GetChains(transaction)

	account := transaction.GetNodeAccount(app)
	role := transaction.GetRole(account)

	return GetCommands(SWAP, role, chains)
}

// Plug in data from the rest of a system into a set of commands
func Resolve(app interface{}, transaction Transaction, commands Commands) Commands {
	swap := transaction.(*Swap)
	account := swap.GetNodeAccount(app)

	identities := GetIdentities(app)
	_ = identities

	utxo := GetUtxo(app)
	_ = utxo

	var iindex, pindex int

	chains := GetChains(transaction)
	for i := 0; i < len(commands); i++ {
		role := swap.GetRole(account)
		if role == INITIATOR {
			commands[i].Chain = chains[0]
			iindex = 0
			pindex = 1
		} else {
			commands[i].Chain = chains[1]
			iindex = 1
			pindex = 0
		}

		commands[i].Data[ROLE] = role
		commands[i].Data[INITIATOR_ACCOUNT] = chains[iindex]
		commands[i].Data[PARTICIPANT_ACCOUNT] = chains[pindex]

		commands[i].Data[AMOUNT] = swap.Amount
		commands[i].Data[EXCHANGE] = swap.Exchange
		commands[i].Data[NONCE] = swap.Nonce
		commands[i].Data[PREIMAGE] = swap.Preimage

		commands[i].Data[PASSWORD] = "password" // TODO: Needs to be corrected
	}
	return commands
}

// Execute the function
func Execute(app interface{}, command Command) err.Code {
	if command.Execute() {
		return err.SUCCESS
	}
	return err.NOT_IMPLEMENTED
}

func CreateContractBTC(context map[Parameter]FunctionValue) bool {
	address := global.Current.BTCAddress
	parts := strings.Split(address, ":")
	port := convert.GetInt(parts[1], 46688)

	role := GetRole(context[ROLE])
	password := GetString(context[PASSWORD])

	_ = role
	_ = password

	cli := bitcoin.GetBtcClient(port)
	_ = cli
	//todo: runCommand(initCmd,cli)

	return true
}

func CreateContractETH(context map[Parameter]FunctionValue) bool {
	return true
}

func CreateContractOLT(context map[Parameter]FunctionValue) bool {
	return true
}
