package kas

import (
	"fmt"
	"github.com/klaytn/klaytn/blockchain"
	"github.com/klaytn/klaytn/blockchain/types"
	"github.com/klaytn/klaytn/common"
	"math/big"
	"strings"
)

var tokenTransferEventHash = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

// splitToWords divides log data to the words.
func splitToWords(data []byte) []common.Hash {
	var words []common.Hash
	for i := 0; i < len(data); i += common.HashLength {
		words = append(words, common.BytesToHash(data[i:i+common.HashLength]))
	}
	return words
}

// wordToAddress trims input word to get address field only.
func wordToAddress(word common.Hash) common.Address {
	return common.BytesToAddress(word[common.HashLength-common.AddressLength:])
}

// transformsLogsToTokenTransfers converts the given event into Klaytn Compatible Token transfers.
func transformsLogsToTokenTransfers(event blockchain.ChainEvent) []*KCTTransfer {
	timestamp := event.Block.Time().Int64()
	var kctTransfers []*KCTTransfer
	for _, log := range event.Logs {
		if len(log.Topics) > 0 && log.Topics[0] == tokenTransferEventHash {
			transfer := transformLogToTokenTransfer(log)
			transfer.Timestamp = timestamp
			kctTransfers = append(kctTransfers, transfer)
		}
	}

	return kctTransfers
}

// transformLogToTokenTransfer converts the given log to Klaytn Compatible Token transfer.
func transformLogToTokenTransfer(log *types.Log) *KCTTransfer {
	// in case of token transfer,
	// case 1:
	//   log.LogTopics[0] = token transfer event hash
	//   log.LogData = concat(fromAddress, toAddress, value)
	// case 2:
	//   log.LogTopics[0] = token transfer event hash
	//   log.LogTopics[1] = fromAddress
	//   log.LogTopics[2] = toAddresss
	//   log.LogData = value
	data := append(log.Topics, splitToWords(log.Data)...)
	from := wordToAddress(data[1])
	to := wordToAddress(data[2])
	value := new(big.Int).SetBytes(data[3].Bytes())

	txLogId := int64(log.BlockNumber)*maxTxCountPerBlock*maxTxLogCountPerTx + int64(log.TxIndex)*maxTxLogCountPerTx + int64(log.Index)

	return &KCTTransfer{
		ContractAddress:  log.Address.Bytes(),
		From:             from.Bytes(),
		To:               to.Bytes(),
		TransactionLogId: txLogId,
		Value:            "0x" + value.Text(16),
		TransactionHash:  log.TxHash.Bytes(),
	}
}

// InsertTokenTransfers inserts token transfers in the given chain event into KAS database.
// The token transfers are divided into chunkUnit because of max number of place holders.
func (r *repository) InsertTokenTransfers(event blockchain.ChainEvent) error {
	tokenTransfers := transformsLogsToTokenTransfers(event)

	chunkUnit := maxPlaceholders / placeholdersPerKCTTransferItem
	var chunks []*KCTTransfer

	for tokenTransfers != nil {
		if placeholdersPerKCTTransferItem*len(tokenTransfers) > maxPlaceholders {
			chunks = tokenTransfers[:chunkUnit]
			tokenTransfers = tokenTransfers[chunkUnit:]
		} else {
			chunks = tokenTransfers
			tokenTransfers = nil
		}

		if err := r.bulkInsertTokenTransfers(chunks); err != nil {
			logger.Error("failed to insertTokenTransfers", "err", err, "numTokenTransfers", len(chunks))
			return err
		}
	}

	return nil
}

// bulkInsertTokenTransfers inserts the given token transfers in multiple rows at once.
func (r *repository) bulkInsertTokenTransfers(tokenTransfers []*KCTTransfer) error {
	if len(tokenTransfers) == 0 {
		logger.Debug("the token transfer list is empty")
		return nil
	}
	var valueStrings []string
	var valueArgs []interface{}

	for _, transfer := range tokenTransfers {
		valueStrings = append(valueStrings, "(?,?,?,?,?,?,?)")

		valueArgs = append(valueArgs, transfer.TransactionLogId)
		valueArgs = append(valueArgs, transfer.From)
		valueArgs = append(valueArgs, transfer.To)
		valueArgs = append(valueArgs, transfer.Value)
		valueArgs = append(valueArgs, transfer.ContractAddress)
		valueArgs = append(valueArgs, transfer.TransactionHash)
		valueArgs = append(valueArgs, transfer.Timestamp)
	}

	rawQuery := `
			INSERT INTO kct_transfers(transactionLogId, fromAddr, toAddr, value, contractAddress, transactionHash, timestamp)
			VALUES %s
			ON DUPLICATE KEY
			UPDATE transactionLogId=transactionLogId`
	query := fmt.Sprintf(rawQuery, strings.Join(valueStrings, ","))

	if _, err := r.db.DB().Exec(query, valueArgs...); err != nil {
		return err
	}
	return nil
}
