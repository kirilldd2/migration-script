package parallel

import (
	"log"
	"time"

	"github.com/Ioloman/migration-script/app/db/mongodb"
	"github.com/Ioloman/migration-script/app/db/mysql"
	"github.com/Ioloman/migration-script/app/models"
)

func worker(inputCh <-chan *[]int32, outputCh chan<- models.WorkerReturn, database string) {
	for {
		localTiming := &models.Timings{Count: 1}
		result := models.WorkerReturn{Stats: localTiming}

		paymentIDs := <-inputCh
		result.PaymentIDs = paymentIDs

		t := time.Now()

		logs, err := mysql.GetLogs(paymentIDs, database)
		if err != nil {
			result.Error = err
			outputCh <- result
			continue
		}
		if len(*logs) == 0 {
			continue
		}
		localTiming.LogsAmount = uint64(len(*logs))
		t = localTiming.SetSelect(t)

		err = mongodb.InsertLogs(logs)
		if err != nil {
			result.Error = err
			outputCh <- result
			continue
		}
		t = localTiming.SetInsert(t)

		err = mysql.DeleteLogs(paymentIDs, database)
		if err != nil {
			for ; err != nil; err = mysql.DeleteLogs(paymentIDs, database) {
				log.Fatalf("Cannot delete logs: %v", err)
			}
		}
		localTiming.SetDelete(t)

		outputCh <- result
	}
}

func Migrate(batchSize int, numWorkers int, printEvery int, database string) error {
	var returnBuffer models.WorkerReturn
	globalTiming := &models.Timings{NumWorkers: uint64(numWorkers)}
	inputCh := make(chan *[]int32, numWorkers)
	outputCh := make(chan models.WorkerReturn, numWorkers)
	lastCount := uint64(0)

	for i := 0; i < numWorkers; i++ {
		go worker(inputCh, outputCh, database)
	}

	for {
		t := time.Now()
		paymentIDs, err := mysql.GetPaymentIDs(batchSize, database)
		if err != nil {
			log.Fatalf("Error querying payment_ids: %v", err)
			continue
		}
		if len(*paymentIDs) == 0 {
			log.Println("Got 0 logs")
			time.Sleep(time.Second * 5)
			continue
		}
		globalTiming.AddSelect(t)

		select {
		case inputCh <- paymentIDs:

		case returnBuffer = <-outputCh:
			if returnBuffer.Error != nil {
				log.Fatalf("got output with error: %v\n", returnBuffer.Error)
				inputCh <- returnBuffer.PaymentIDs
			} else {
				globalTiming.Add(returnBuffer.Stats)
			}
		}

		if globalTiming.Count%uint64(printEvery) == 0 && globalTiming.Count != lastCount {
			lastCount = globalTiming.Count
			log.Printf("globalTiming: %v\n", globalTiming)
		}
	}
}
