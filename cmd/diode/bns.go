// Diode Network Client
// Copyright 2019 IoT Blockchain Technology Corporation LLC (IBTC)
// Licensed under the Diode License, Version 1.0
package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/diodechain/diode_go_client/command"
	"github.com/diodechain/diode_go_client/config"
	"github.com/diodechain/diode_go_client/contract"
	"github.com/diodechain/diode_go_client/edge"
	"github.com/diodechain/diode_go_client/rpc"
	"github.com/diodechain/diode_go_client/util"
)

var (
	bnsCmd = &command.Command{
		Name:        "bns",
		HelpText:    `  Register/Update name service on diode blockchain.`,
		ExampleText: `  diode bns -register hello-world=0x......`,
		Run:         bnsHandler,
		Type:        command.OneOffCommand,
	}
	bnsPattern = regexp.MustCompile(`^[0-9a-z-]+$`)
)

func init() {
	cfg := config.AppConfig
	bnsCmd.Flag.StringVar(&cfg.BNSRegister, "register", "", "Register a new BNS name with <name>=<address>.")
	bnsCmd.Flag.StringVar(&cfg.BNSUnregister, "unregister", "", "Free a new BNS name with <name>.")
	bnsCmd.Flag.StringVar(&cfg.BNSTransfer, "transfer", "", "Transfer an existing BNS name with <name>=<new_owner>.")
	bnsCmd.Flag.StringVar(&cfg.BNSLookup, "lookup", "", "Lookup a given BNS name.")
}

func isValidBNS(name string) (isValid bool) {
	if len(name) < 7 || len(name) > 32 {
		isValid = false
		return
	}
	isValid = bnsPattern.Match([]byte(name))
	return
}

func bnsHandler() (err error) {
	err = app.Start()
	if err != nil {
		return
	}
	client := app.datapool.GetNearestClient()
	// register bns record
	bn, _ := client.GetBlockPeak()
	if bn == 0 {
		printError("Cannot find block peak: ", fmt.Errorf("not found"))
		return
	}

	var done bool

	if done, err = handleRegister(); done || err != nil {
		return
	}
	if done, err = handleUnregister(); done || err != nil {
		return
	}
	if done, err = handleTransfer(); done || err != nil {
		return
	}
	if done, err = handleLookup(); done || err != nil {
		return
	}

	printError("Argument Error: ", fmt.Errorf("provide -register <name>=<address> or -lookup <name> or -unregister <name> or -transfer <name>=<new_owner> argument"))
	return
}

func handleLookup() (done bool, err error) {
	cfg := config.AppConfig
	lookupName := strings.ToLower(cfg.BNSLookup)
	if len(lookupName) == 0 {
		return
	}
	done = true

	var obnsAddr util.Address
	var ownerAddr util.Address
	client := app.datapool.GetNearestClient()
	obnsAddr, err = client.ResolveBNS(lookupName)
	if err != nil {
		printError("Lookup error: ", err)
		return
	}
	printLabel("Lookup result: ", fmt.Sprintf("%s=0x%s", lookupName, obnsAddr.Hex()))
	ownerAddr, err = client.ResolveBNSOwner(lookupName)
	if err != nil {
		printError("Couldn't lookup owner: ", err)
		return
	}
	printLabel("Domain owner: ", fmt.Sprintf("0x%s", ownerAddr.Hex()))
	return
}

func handleRegister() (done bool, err error) {
	cfg := config.AppConfig
	if len(cfg.BNSRegister) == 0 {
		done = false
		return
	}
	registerPair := strings.Split(cfg.BNSRegister, "=")
	done = true

	client := app.datapool.GetNearestClient()
	var bnsContract contract.BNSContract
	bnsContract, err = contract.NewBNSContract()
	if err != nil {
		printError("Cannot create BNS contract instance: ", err)
		return
	}

	var obnsAddr util.Address
	// should lowercase bns name
	bnsName := strings.ToLower(registerPair[0])
	if !isValidBNS(bnsName) {
		printError("Argument Error: ", fmt.Errorf("BNS name should be more than 7 or less than 32 characters (0-9A-Za-z-)"))
		return
	}
	nonce := client.GetAccountNonce(0, cfg.ClientAddr)
	var bnsAddr util.Address
	if len(registerPair) > 1 {
		bnsAddr, err = util.DecodeAddress(registerPair[1])
		if err != nil {
			printError("Invalid diode address", err)
			return
		}
	} else {
		bnsAddr = cfg.ClientAddr
	}
	// check bns
	obnsAddr, err = client.ResolveBNS(bnsName)
	if err == nil {
		if obnsAddr == bnsAddr {
			printError("BNS name is already mapped to this address", err)
			return
		}
	}
	// send register transaction
	var res bool
	registerData, _ := bnsContract.Register(bnsName, bnsAddr)
	ntx := edge.NewTransaction(nonce, 0, 10000000, contract.BNSAddr, 0, registerData, 0)
	res, err = client.SendTransaction(ntx)
	if err != nil {
		printError("Cannot register blockchain name service: ", err)
		return
	}
	if !res {
		printError("Cannot register blockchain name service: ", fmt.Errorf("server return false"))
		return
	}
	printLabel("Register bns: ", fmt.Sprintf("%s=%s", bnsName, bnsAddr.HexString()))
	wait(client, func() bool {
		current, err := client.ResolveBNS(bnsName)
		return err == nil && current == bnsAddr
	})
	return
}

func handleTransfer() (done bool, err error) {
	cfg := config.AppConfig
	transferPair := strings.Split(cfg.BNSTransfer, "=")

	if len(transferPair) != 2 {
		done = false
		return
	}
	done = true

	client := app.datapool.GetNearestClient()
	var bnsContract contract.BNSContract
	bnsContract, err = contract.NewBNSContract()
	if err != nil {
		printError("Cannot create BNS contract instance: ", err)
		return
	}

	bnsName := strings.ToLower(transferPair[0])
	if !isValidBNS(bnsName) {
		printError("Argument Error: ", fmt.Errorf("BNS name should be more than 7 or less than 32 characters (0-9A-Za-z-)"))
		return
	}
	nonce := client.GetAccountNonce(0, cfg.ClientAddr)
	var newOwner util.Address

	newOwner, err = util.DecodeAddress(transferPair[1])
	if err != nil {
		printError("Invalid destination address", err)
		return
	}

	// check bns
	var owner rpc.Address
	owner, err = client.ResolveBNSOwner(bnsName)
	if err == nil {
		if owner == newOwner {
			err = fmt.Errorf("domain is already owned by %v", owner.HexString())
			printError("BNS name already transferred", err)
			return
		}
		if owner != client.Config.ClientAddr {
			err = fmt.Errorf("bns domain is owned by %v", owner.HexString())
			printError("BNS name can't be transfered", err)
			return
		}
	}

	// send register transaction
	var res bool
	registerData, _ := bnsContract.Transfer(bnsName, newOwner)
	ntx := edge.NewTransaction(nonce, 0, 10000000, contract.BNSAddr, 0, registerData, 0)
	res, err = client.SendTransaction(ntx)
	if err == nil && !res {
		err = fmt.Errorf("server returned false")
	}
	if err != nil {
		printError("Cannot transfer blockchain name: ", err)
		return
	}
	printLabel("Transferring bns: ", fmt.Sprintf("%s=%s", bnsName, newOwner.HexString()))
	wait(client, func() bool {
		current, err := client.ResolveBNSOwner(bnsName)
		return err == nil && current == newOwner
	})
	return
}

func handleUnregister() (done bool, err error) {
	cfg := config.AppConfig
	if len(cfg.BNSUnregister) == 0 {
		done = false
		return
	}
	done = true

	client := app.datapool.GetNearestClient()
	var bnsContract contract.BNSContract
	bnsContract, err = contract.NewBNSContract()
	if err != nil {
		printError("Cannot create BNS contract instance: ", err)
		return
	}

	bnsName := strings.ToLower(cfg.BNSUnregister)
	if !isValidBNS(bnsName) {
		printError("Argument Error: ", fmt.Errorf("BNS name should be more than 7 or less than 32 characters (0-9A-Za-z-)"))
		return
	}
	nonce := client.GetAccountNonce(0, cfg.ClientAddr)

	// check bns
	var owner rpc.Address
	owner, _ = client.ResolveBNSOwner(bnsName)
	if owner == [20]byte{} {
		err = fmt.Errorf("BNS name is already free")
		return
	} else if owner != client.Config.ClientAddr {
		err = fmt.Errorf("BNS owned by %v", owner.HexString())
		printError("BNS name can't be freed", err)
		return
	}

	// send register transaction
	var res bool
	registerData, _ := bnsContract.Unregister(bnsName)
	ntx := edge.NewTransaction(nonce, 0, 10000000, contract.BNSAddr, 0, registerData, 0)
	res, err = client.SendTransaction(ntx)
	if err == nil && !res {
		err = fmt.Errorf("server returned false")
	}
	if err != nil {
		printError("Cannot unregister blockchain name: ", err)
		return
	}
	printLabel("Unregistering bns: ", bnsName)
	wait(client, func() bool {
		owner, _ := client.ResolveBNSOwner(bnsName)
		return owner == [20]byte{}
	})
	return
}

func wait(client *rpc.RPCClient, condition func() bool) {
	printInfo("Waiting for block to be confirmed - expect to wait 5 minutes")
	for i := 0; i < 6000; i++ {
		bn, _ := client.LastValid()
		if condition() {
			printInfo("Transaction executed successfully!")
			return
		}
		for {
			bn2, _ := client.LastValid()
			if bn != bn2 {
				break
			}
			time.Sleep(time.Millisecond * 100)
		}
	}
	printError("Giving up to wait for transaction", fmt.Errorf("timeout after 10 minutes"))
}
