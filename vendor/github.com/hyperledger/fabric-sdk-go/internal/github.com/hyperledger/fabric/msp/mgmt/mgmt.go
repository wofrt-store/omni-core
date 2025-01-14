/*
Copyright IBM Corp. 2017 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

		 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
/*
Notice: This file has been modified for Hyperledger Fabric SDK Go usage.
Please review third_party pinning scripts and patches for more details.
*/

package mgmt

import (
	"sync"

	"github.com/hyperledger/fabric-sdk-go/internal/github.com/hyperledger/fabric/msp"
	"github.com/hyperledger/fabric-sdk-go/internal/github.com/hyperledger/fabric/msp/cache"
	flogging "github.com/hyperledger/fabric-sdk-go/internal/github.com/hyperledger/fabric/sdkpatch/logbridge"
	"github.com/spf13/viper"
)

// FIXME: AS SOON AS THE CHAIN MANAGEMENT CODE IS COMPLETE,
// THESE MAPS AND HELPSER FUNCTIONS SHOULD DISAPPEAR BECAUSE
// OWNERSHIP OF PER-CHAIN MSP MANAGERS WILL BE HANDLED BY IT;
// HOWEVER IN THE INTERIM, THESE HELPER FUNCTIONS ARE REQUIRED

var m sync.Mutex
var localMsp msp.MSP
var mspMap map[string]msp.MSPManager = make(map[string]msp.MSPManager)
var mspLogger = flogging.MustGetLogger("msp")

// GetLocalMSP returns the local msp (and creates it if it doesn't exist)
func GetLocalMSP() msp.MSP {
	var lclMsp msp.MSP
	var created bool = false
	{
		// determine the type of MSP (by default, we'll use bccspMSP)
		mspType := viper.GetString("peer.localMspType")
		if mspType == "" {
			mspType = msp.ProviderTypeToString(msp.FABRIC)
		}

		// based on the MSP type, generate the new opts
		var newOpts msp.NewOpts
		switch mspType {
		case msp.ProviderTypeToString(msp.FABRIC):
			newOpts = &msp.BCCSPNewOpts{NewBaseOpts: msp.NewBaseOpts{Version: msp.MSPv1_0}}
		case msp.ProviderTypeToString(msp.IDEMIX):
			newOpts = &msp.IdemixNewOpts{msp.NewBaseOpts{Version: msp.MSPv1_1}}
		default:
			panic("msp type " + mspType + " unknown")
		}

		m.Lock()
		defer m.Unlock()

		lclMsp = localMsp
		if lclMsp == nil {
			var err error
			created = true

			mspInst, err := msp.New(newOpts)
			if err != nil {
				mspLogger.Fatalf("Failed to initialize local MSP, received err %+v", err)
			}

			switch mspType {
			case msp.ProviderTypeToString(msp.FABRIC):
				lclMsp, err = cache.New(mspInst)
				if err != nil {
					mspLogger.Fatalf("Failed to initialize local MSP, received err %+v", err)
				}
			case msp.ProviderTypeToString(msp.IDEMIX):
				lclMsp = mspInst
			default:
				panic("msp type " + mspType + " unknown")
			}
			localMsp = lclMsp
		}
	}

	if created {
		mspLogger.Debugf("Created new local MSP")
	} else {
		mspLogger.Debugf("Returning existing local MSP")
	}

	return lclMsp
}
