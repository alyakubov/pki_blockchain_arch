package main

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gorilla/mux"
	//"math/rand"
)

func init() {
	err := LoadConfig()
	if err != nil {
		fmt.Printf("CONFIG ERROR: %v\n", err)
		os.Exit(1)
	}
	gConfig.WebMode = 1

}

func main() {

	gPrivateKey = new(rsa.PrivateKey)
	err := LoadPrivateKey(gConfig.PrivateKeyPath, gPrivateKey)
	if err != nil {
		gPrivateKey = nil
		log.Printf("Private key is not loaded, omitting : %v\n", gConfig.PrivateKeyPath)
	} else {
		log.Printf("Private key successfully loaded : %v\n", gConfig.PrivateKeyPath)
	}

	// how to connect to different servers
	//          (https://gist.github.com/bas-vk/299f4a686b66a22cf87302c561ee5866):
	//    geth --testnet --rpc
	// client, err := ethclient.Dial("http://localhost:8545")
	//    parity --testnet --port 31313 --jsonrpc-port 8546
	// client, err = ethclient.Dial("http://localhost:8546")

	// http://stackoverflow.com/questions/15834278/serving-static-content-with-a-root-url-with-the-gorilla-toolkit
	// subrouter - http://stackoverflow.com/questions/18720526/how-does-pathprefix-work-in-gorilla-mux-library-for-go
	r := mux.NewRouter()
	//r.HandleFunc("/pki-test", PkiForm);
	r.HandleFunc("/enroll_user", rstEnrollUser)
	r.HandleFunc("/blacklist_user", rstBlacklistUser)
	//r.HandleFunc("/enroll_ca", EnrollCA);
	r.HandleFunc("/create_contract", rstCreateContract)
	r.HandleFunc("/populate_contract", rstPopulateContract)
	//r.HandleFunc("/validate_form", ValidateForm);
	r.HandleFunc("/validate_cert", rstValidateCert)
	r.HandleFunc("/download_cacert", rstDownloadCaCert)
	//r.HandleFunc("/generate_user_cert", GenerateUserCert);

	fs := http.FileServer(http.Dir("/home/alex/DocsFS/Dropbox/WORK/RD/LuxBCh/PKI/public"))
	spref := http.StripPrefix("/public/", fs)
	r.PathPrefix("/public/").Handler(spref)
	http.Handle("/", r)

	//https://gist.github.com/denji/12b3a568f092ab951456 - SSL info
	//https://golanglibs.com/top?q=webrtc - webrtc server side for golang

	//var server = &http.Server{
	//    Addr : ":8071",
	//    Handler : r,
	//}

	log.Println("RESTful service is listening...")
	//http.ListenAndServeTLS(":8071", "server.pem", "server.key", r)
	http.ListenAndServe(":"+strconv.Itoa(gConfig.RestHttpPort), nil)
}

/*
/blacklist_user, all parameters are in POST
Puts certificate (either ordinary or CA) from the white list to the black list
	Parameters:
		ParentAddr: the address of the CA smart contract where the certificate's hash is stored
		UserAddr: the ID (address) of the user who has the privilage to modify the smart contract.
			The key of this user should be available in key storage
		Deletion: array of strings with IDs of the items to be deleted in the user list.
			It is produced with checkbox HTML forms
	Returns:
		200 and "OK" in the html body in case of success
		Errors (details are in html body):
			484 : ParentAddr is incorrect
			485 : Deletion is incorrect
			580 : Ethereum executionn error (out of gas and others)
			581 : Ethereum connection error
			500 : Other error
*/
func rstBlacklistUser(w http.ResponseWriter, r *http.Request) {
	var revokeResult string
	var parentAddr common.Address = common.Address{}
	var userAddr common.Address = common.Address{}

	//fmt.Println("DEBUG REST: inside blacklist")
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		fmt.Printf("No data: Parsing blacklist multipart form: %v\n", err.Error())
		http.Error(w, GeneralError{fmt.Sprintf(
			"BlacklistUser: error in parsing -- ", err.Error())}.Error(),
			http.StatusInternalServerError)
		return
	}

	strParentAddrArr := r.MultipartForm.Value["ParentAddr"]
	if len(strParentAddrArr) > 0 {
		if common.IsHexAddress(strParentAddrArr[0]) == false {
			http.Error(w, GeneralError{"Contract address is incorrect"}.Error(),
				484 /*http.StatusInternalServerError*/)
			return
		}
		parentAddr = common.HexToAddress(strParentAddrArr[0])
	}

	if (parentAddr == common.Address{}) {
		http.Error(w, GeneralError{"Delete: Parent address is not established"}.Error(),
			484 /*http.StatusInternalServerError*/)
		return
	}

	strUserAddrArr := r.MultipartForm.Value["UserAddr"]
	if len(strUserAddrArr) > 0 {
		if common.IsHexAddress(strUserAddrArr[0]) == false {
			http.Error(w, GeneralError{"User address is incorrect"}.Error(),
				http.StatusInternalServerError)
			return
		}
		userAddr = common.HexToAddress(strUserAddrArr[0])
	}

	dels := r.MultipartForm.Value["Deletion"]
	if len(dels) > 0 {
		//fmt.Printf("Debug: I am in deletion block")
		//dels := r.MultipartForm.Value["Deletion"]
		//dels := r.Form["Deletion"]
		for _, del := range dels {
			fmt.Printf("Rest Debug: del=%v\n", del)
			delid, err := strconv.Atoi(del)
			if err != nil {
				http.Error(w, fmt.Sprintf("Deletion conversion error: %v", err.Error()),
					485 /*http.StatusInternalServerError*/)
				return
			}
			//revokedParam.RevokedIds = append(revokedParam.RevokedIds, delid);
			revokeResult += del + " "

			client, err := ethclient.Dial(gConfig.IPCpath)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to connect to the Ethereum client: %v", err),
					581 /*http.StatusInternalServerError*/)
				return
			}

			// Instantiate the contract, the address is taken from eth at the moment of contract initiation
			// kyc, err := NewLuxUni_KYC(common.HexToAddress(gContractHash), backends.NewRPCBackend(conn))
			pkiContract, err := NewLuxUni_PKI(parentAddr, client)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to instantiate a smart contract: %v", err),
					581 /*http.StatusInternalServerError*/)
				return
			}

			// Logging into Ethereum as a user
			if (userAddr == common.Address{}) {
				fmt.Printf("Attention! Revoke: user address is zero, default config account is used\n")
				userAddr = common.HexToAddress(gConfig.AccountAddr)
			}
			keyFile, err := FindKeyFile(userAddr)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to find key file for account %v. %v ",
					userAddr.String(), err), 581 /*http.StatusInternalServerError*/)
				return
			}
			key, err := ioutil.ReadFile(gConfig.KeyDir + keyFile)
			if err != nil {
				http.Error(w, fmt.Sprintf("Key File error: %v\n", err),
					581 /*http.StatusInternalServerError*/)
				return
			}

			auth, err := bind.NewTransactor(strings.NewReader(string(key)), gConfig.Pswd)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to create authorized transactor: %v", err),
					581 /*http.StatusInternalServerError*/)
				return
			}

			sess := &LuxUni_PKISession{
				Contract: pkiContract,
				CallOpts: bind.CallOpts{
					Pending: true,
				},
				TransactOpts: bind.TransactOpts{
					From:     auth.From,
					Signer:   auth.Signer,
					GasLimit: big.NewInt(2000000),
				},
			}
			/* sess.TransactOpts = *auth
			sess.TransactOpts.GasLimit = big.NewInt(2000000) // Rinkeby block gas limit 6124970 */

			_, nerr := sess.DeleteRegDatum(big.NewInt(int64(delid)))
			if nerr != nil {
				http.Error(w, fmt.Sprintf("Deletion error: %v", nerr),
					580 /*http.StatusInternalServerError*/)
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

/*
/enroll_user, all parameters in POST
	Parameters:
		Hash or UplFiles (hash is a hex string without a leading "0x")
		UplFiles : uploaded certificate
		ParentAddr: the address of the CA smart contract where the certificate's hash is stored
			This address of this contract should be called at user account CurrentUserAddr
		CurrentUserAddr: the ID (address) of the user who has the privilage to modify the parent smart contract.
			The key of this user should be available in key storage
	Returns:
		200 and "OK" in the html body in case of success
		Errors (details are in html body):
			480 : hash has the wrong length or hash is incorrect
			481 : hash is already enrolled
			482 : Certificate errors in case it was provided instead of hash
			484 : ParentAddr is incorrect
			485 : CurrentUserAddr is incorrect
			580 : Ethereum execution error (out of gas and others)
			581 : Ethereum connection error
			500 : Other error
    Not used paremeters which were deleited and used in web application to store the data for CA tree navigation
    	// ContrAddr - REMOVED AS WEB APP STORES IT ITSELF
    	// NewUserAddr -- corresponds to userAddr associated with new contract -- REMOVED AS WEB APP STORES IT ITSELF
*/
func rstEnrollUser(w http.ResponseWriter, r *http.Request) {
	var parentAddr common.Address = common.Address{} // this is addr of the contract which is going to hold the hash
	// REMOVED - var contrAddr common.Address = common.Address{}   // this is address of the new SubCA contract or zero if end user
	var curUserAddr common.Address = common.Address{} // !! this is the user_id of the owner of parent contr
	// REMOVED var newUserAddr common.Address = common.Address{} // !! this is the new owner of contrAddr contr.
	var isNoUpload bool = false

	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		fmt.Printf("EnrollUser: No change data -- ", err.Error())
		http.Error(w, GeneralError{fmt.Sprintf(
			"EnrollUser: No change data -- ", err.Error())}.Error(),
			http.StatusInternalServerError)
		return
	}

	hashSum, _ /*fileName*/, dataCert, cerr :=
		UploadFile(w, r, "UplFiles", true)
	if cerr.errCode != 0 {
		if cerr.errCode == 1 {
			isNoUpload = true
			hashArr := r.MultipartForm.Value["Hash"]
			if len(hashArr) == 0 {
				http.Error(w, GeneralError{fmt.Sprintf(
					"EnrollUser: No hashes in request")}.Error(),
					480 /* http.StatusInternalServerError */)
				return
			}

			hashInt := big.NewInt(0)
			hashInt, _ = hashInt.SetString(hashArr[0], 16) /* tmpInt, err := strconv.Atoi(hashArr[0]); */
			if hashInt == nil {
				http.Error(w, fmt.Sprintf("EnrollUser: Hash string %s is incorrect", hashArr[0]),
					480 /*http.StatusInternalServerError*/)
				return
			}
			hashSum = hashInt.Bytes()
		} else {
			http.Error(w, GeneralError{fmt.Sprintf(
				"EnrollUser UplFiles:", cerr.Error())}.Error(),
				482 /*http.StatusInternalServerError*/)
			return
		}
	}

	strParentAddrArr := r.MultipartForm.Value["ParentAddr"]
	if len(strParentAddrArr) > 0 {
		if common.IsHexAddress(strParentAddrArr[0]) == false {
			http.Error(w, fmt.Sprintf("Parent address as a parameter is incorrect: %v",
				strParentAddrArr[0]),
				484 /*http.StatusInternalServerError*/)
			return
		}
		parentAddr = common.HexToAddress(strParentAddrArr[0])
	}

	if isNoUpload == false {
		var caContrAddr, insertAddr common.Address
		caContrAddr, insertAddr, _ /*desc*/, err = ParseCert(dataCert)
		if err != nil {
			http.Error(w, fmt.Sprintf("CERTIFICATE: Parsing error: %v", err),
				482 /*http.StatusInternalServerError*/)
			return
		}
		if (insertAddr == common.Address{}) {
			http.Error(w, "CERTIFICATE: No Parent Address is provided in the Cert",
				482 /*http.StatusInternalServerError*/)
			return
		}
		if (caContrAddr != common.Address{}) {
			http.Error(w, "CERTIFICATE: Non-CA certificates should not include non-zero CA contract address",
				482 /*http.StatusInternalServerError*/)
			return
		}
		if insertAddr != parentAddr {
			http.Error(w, "Address in the certificate does not correspond to the contract address of the Authority (CA)",
				482 /*http.StatusInternalServerError*/)
			return
		}
	}

	strUserAddrArr := r.MultipartForm.Value["CurrentUserAddr"]
	if len(strUserAddrArr) > 0 {
		if common.IsHexAddress(strUserAddrArr[0]) == false {
			http.Error(w, GeneralError{"CurrentUser address is incorrect"}.Error(),
				485 /*http.StatusInternalServerError*/)
			return
		}
		curUserAddr = common.HexToAddress(strUserAddrArr[0])
	}

	// REMOVED AS WEB APP STORES ContrAddr ITSELF
	/*
		strUserAddrArr = r.MultipartForm.Value["NewUserAddr"]
		if len(strUserAddrArr) > 0 {
			if common.IsHexAddress(strUserAddrArr[0]) == false {
				http.Error(w, GeneralError{"NewUser address is incorrect"}.Error(),
					http.StatusInternalServerError)
				return
			}
			newUserAddr = common.HexToAddress(strUserAddrArr[0])
		} */

	// REMOVED AS WEB APP STORES ContrAddr ITSELF
	/*
		strContrAddrArr := r.MultipartForm.Value["ContrAddr"]
		if len(strContrAddrArr) > 0 {
			if common.IsHexAddress(strContrAddrArr[0]) == false {
				http.Error(w, GeneralError{"Contract address is incorrect"}.Error(),
					http.StatusInternalServerError)
				return
			}
			contrAddr = common.HexToAddress(strContrAddrArr[0])
		} */

	/*
	   if (contrAddr!=common.Address{} && certCnt!= nil && parentAddr==common.Address{}) {
	       parentAddr := common.Address{}
	       if len(certCnt)< (gCaCertOffset+len( parentAddr.Bytes() )){
	           http.Error(w, GeneralError{fmt.Sprintf(
	               "EnrollUser: Certificate is too small")}.Error(),
	               http.StatusInternalServerError)
	           return
	       }
	       // TO DO: ADD a check if the chunk below is an address with common.isAddress (isHex)
	       parentAddr.SetBytes( certCnt[gCaCertOffset : gCaCertOffset+len( contrAddr.Bytes() )] )
	   }
	*/

	if (parentAddr == common.Address{}) {
		http.Error(w, GeneralError{"Enroll: Parent address is not established"}.Error(),
			484 /*http.StatusInternalServerError*/)
		return
	}

	// fmt.Printf("DEBUG before newRegDatum: fname=%v, desc=%v \n", fileName, desc)

	client, err := ethclient.Dial(gConfig.IPCpath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Enroll: Failed to connect to the Ethereum client: %v", err),
			581 /*http.StatusInternalServerError*/)
		return
	}

	// Instantiate the contract, the address is taken from eth at the moment of contract initiation
	// kyc, err := NewLuxUni_KYC(common.HexToAddress(gContractHash), backends.NewRPCBackend(conn))
	pkiContract, err := NewLuxUni_PKI(parentAddr, client)
	if err != nil {
		http.Error(w, fmt.Sprintf("Enroll: Failed to instantiate a smart contract: %v", err),
			581 /*http.StatusInternalServerError*/)
		return
	}

	callOpts := &bind.CallOpts{
		Pending: true,
	}
	initNumRegData, err := pkiContract.GetNumRegData(callOpts)
	if err != nil {
		http.Error(w, fmt.Sprintf("EnrollUser: Failed to get numRegData from blockchain: %v. ", err),
			580 /*http.StatusInternalServerError*/)
		return
	}

	// Logging into Ethereum as a user
	if (curUserAddr == common.Address{}) {
		fmt.Printf("Attention! Enroll: user address is zero, default config account is used\n")
		curUserAddr = common.HexToAddress(gConfig.AccountAddr)
	}
	keyFile, err := FindKeyFile(curUserAddr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to find key file for account %v. %v ",
			curUserAddr.String(), err), 581 /*http.StatusInternalServerError*/)
		return
	}
	key, err := ioutil.ReadFile(gConfig.KeyDir + keyFile)
	if err != nil {
		http.Error(w, fmt.Sprintf("Enroll: Ethereum connect -- Key File error: %v\n", err),
			581 /*http.StatusInternalServerError*/)
		return
	}
	//fmt.Printf("DEBUG: Found Ethereum Key File \n")

	auth, err := bind.NewTransactor(strings.NewReader(string(key)), gConfig.Pswd)
	if err != nil {
		http.Error(w, fmt.Sprintf("Enroll: Failed to create authorized transactor: %v", err),
			581 /*http.StatusInternalServerError*/)
		return
	}

	sess := &LuxUni_PKISession{
		Contract: pkiContract,
		CallOpts: bind.CallOpts{
			Pending: true,
		},
		TransactOpts: bind.TransactOpts{
			From:     auth.From,
			Signer:   auth.Signer,
			GasLimit: big.NewInt(2000000),
		},
	}
	/*sess.TransactOpts = *auth
	sess.TransactOpts.GasLimit = big.NewInt(2000000) // Rinkeby block gas limit 6124970
	sess.TransactOpts.Nonce = nil  // can help with Rinkeby error of removing pending transaction*/

	var tmpHash [32]byte
	copy(tmpHash[:], hashSum)
	res, err := sess.NewRegDatum(tmpHash, []byte("")) /* contrAddr, fileName, desc, "", newUserAddr */
	/*var trOpts bind.TransactOpts = *auth
	trOpts.GasLimit = big.NewInt(500000)                              // 6124970 - block gas limit in Rinkeby
	res, err := pkiContract.NewRegDatum(&trOpts, tmpHash, []byte("")) /* contrAddr, fileName, desc, "", newUserAddr */
	if err != nil {
		http.Error(w, fmt.Sprintf("EnrollUser: Failed to add a record to blockchain: %v. ", err),
			580 /*http.StatusInternalServerError*/)
		return
	}

	finalNumRegData, err := pkiContract.GetNumRegData(callOpts)
	if err != nil {
		http.Error(w, fmt.Sprintf("EnrollUser: Failed to get numRegData from blockchain: %v. ", err),
			580 /*http.StatusInternalServerError*/)
		return
	}

	if finalNumRegData.Int64() != initNumRegData.Int64()+1 {
		http.Error(w, fmt.Sprintf("EnrollUser: Failed to add a record, wrong function return: %х",
			res.Data()), 580 /*http.StatusInternalServerError*/)
		return
	}

	/*!!!!!*/ // var result uint64 = uint64(finalNumRegData.Int64() - 1)
	result, err := GetEventReturn(tmpHash, parentAddr)
	if err != nil {
		http.Error(w, fmt.Sprintf("EnrollUser: Failed to retreive result: %v",
			err), 580) //*http.StatusInternalServerError
		return
	}
	/*!!!!!*/ //result = uint64(finalNumRegData.Int64() - 1)

	if result != uint64(finalNumRegData.Int64()-1) {
		http.Error(w, fmt.Sprintf("EnrollUser: Retreived result does not correspond to Number of RegData",
			err), 580) //*http.StatusInternalServerError
		return
	}

	// UplFile is id in the input "file" component of the form
	// http://stackoverflow.com/questions/33771167/handle-file-uploading-with-go
	// file, handler, err := r.FormFile("UplFile")
	//out, err := os.Create("/tmp/tst_"+handler.Filename);

	w.WriteHeader(http.StatusOK)
	//w.Write([]byte(`{"arrayInd": ` + strconv.Itoa(int(finalNumRegData.Int64()-1)) + ` }`))
	w.Write([]byte(strconv.Itoa(int(result))))
	//fmt.Printf("Rest Enroll: %v", strconv.Itoa(int(finalNumRegData.Int64())))
}

/*
/populate_contract, all parameters in POST
	Pupulation of the CA smart contract:
		a. putting a certificate into the contract referencing its parent, and
		b. setting ownership of the smartcontract to the user
	Params:
		UplFiles : uploaded certificate
		NewUserAddr - owner is set to this address at the end of the proc. If empty, then new owner is not set
			At the end of the population procedure only the NewUserAddr can modify the smart contract in the future
		CurrentUserAddr: - the user addr to connect to Ethereum. If empty, then set to root user addr
		ContrAddr: the address of the CA smart contract which should be populated
			This address of this contract should be called at user account CurrentUserAddr
	Returns:
		200 and hash string WITHOUT heading "0x" in the html body in case of success
		Errors (details are in html body):
			482 : Certificate errors
			483 : NewUserAddr is incorrect
			484 : ContrAddr is incorrect
			485 : CurrentUserAddr is incorrect
			580 : Ethereum execution error (out of gas and others)
			581 : Ethereum connection error
			500 : Other error
*/
func rstPopulateContract(w http.ResponseWriter, r *http.Request) {

	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, GeneralError{"Rest populate: No change data: Parsing multipart form: %v"}.Error(),
			http.StatusInternalServerError)
		return
	}

	//isCurl = r.MultipartForm.Value["Curl"]
	if len(r.MultipartForm.Value["ContrAddr"]) == 0 {
		http.Error(w, GeneralError{"Rest populate: No contrAddr is provided"}.Error(),
			484 /*http.StatusInternalServerError*/)
		return
	}
	contrAddrStr := r.MultipartForm.Value["ContrAddr"][0]
	if common.IsHexAddress(contrAddrStr) == false {
		http.Error(w, GeneralError{"Rest populate: Contract address is incorrect"}.Error(),
			484 /*http.StatusInternalServerError*/)
		return
	}
	contrAddr := common.HexToAddress(contrAddrStr)

	newUserAddr := common.Address{}
	if len(r.MultipartForm.Value["NewUserAddr"]) != 0 {
		userAddrStr := r.MultipartForm.Value["NewUserAddr"][0]
		if common.IsHexAddress(userAddrStr) == false {
			http.Error(w, GeneralError{"Rest populate: New User address is incorrect"}.Error(),
				483 /*http.StatusInternalServerError*/)
			return
		}
		newUserAddr = common.HexToAddress(userAddrStr)
	} /*else {
		http.Error(w, "New User address is not available in params", http.StaCurrenttusInternalServerError)
		return
	}*/

	curUserAddr := common.Address{}
	if len(r.MultipartForm.Value["CurrentUserAddr"]) != 0 {
		userAddrStr := r.MultipartForm.Value["CurrentUserAddr"][0]
		if common.IsHexAddress(userAddrStr) == false {
			http.Error(w, GeneralError{"Current User address is incorrect"}.Error(),
				http.StatusInternalServerError)
			return
		}
		curUserAddr = common.HexToAddress(userAddrStr)
	} /*else {
		http.Error(w, "Current User address is not available in params", http.StatusInternalServerError)
		return
	}*/

	hashCert, _, dataCert, cerr := UploadFile(w, r, "UplFiles", true)
	if cerr.errCode != 0 {
		fmt.Printf(fmt.Sprintf("Rest Populate: Uploadfile: %v\n", cerr.Error()))
		http.Error(w, cerr.Error(), 482 /*http.StatusInternalServerError*/)
		return
	}
	/*
	   dataCert, err := GenerateCert(contrAddr, parentAddr, true, "Mother Nature CA")
	   if err != nil {
	       http.Error(w, err.Error(), http.StatusInternalServerError)
	       return
	   }

	   hashCert, err := CalcHash(dataCert)
	   if err != nil {
	       http.Error(w, err.Error(), http.StatusInternalServerError)
	       return
	   }
	*/

	client, err := ethclient.Dial(gConfig.IPCpath)
	if err != nil {
		http.Error(w, err.Error(), 581 /*http.StatusInternalServerError*/)
		return
	}

	// Instantiate the contract, the address is taken from eth at the moment of contract initiation
	// kyc, err := NewLuxUni_KYC(common.HexToAddress(gContractHash), backends.NewRPCBackend(conn))
	pkiContract, err := NewLuxUni_PKI(contrAddr, client)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Populate: Failed to instantiate a smart contract: %v", err),
			581 /*http.StatusInternalServerError*/)
		return
	}

	// Logging into Ethereum as a user
	if (curUserAddr == common.Address{}) {
		fmt.Printf("Attention! Populate contract: user address is zero, default config account is used\n")
		curUserAddr = common.HexToAddress(gConfig.AccountAddr)
	}
	keyFile, err := FindKeyFile(curUserAddr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Populate: Failed to find key file for account %v. %v ",
			curUserAddr.String(), err), 581 /*http.StatusInternalServerError*/)
		return
	}
	key, err := ioutil.ReadFile(gConfig.KeyDir + keyFile)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Populatre: Key File %v error: %v\n",
			gConfig.KeyDir+keyFile, err), 581 /*http.StatusInternalServerError*/)
		return
	}
	fmt.Printf("Found Ethereum Key File \n")

	auth, err := bind.NewTransactor(strings.NewReader(string(key)), gConfig.Pswd)
	if err != nil {
		log.Fatalf("Failed to create authorized transactor: %v", err)
		http.Error(w, fmt.Sprintf("Rest Populatre: Failed to create authorized transactor: %v", err),
			581 /*http.StatusInternalServerError*/)
		return
	}

	sess := &LuxUni_PKISession{
		Contract: pkiContract,
		CallOpts: bind.CallOpts{
			Pending: true,
		},
		TransactOpts: bind.TransactOpts{
			From:     auth.From,
			Signer:   auth.Signer,
			GasLimit: big.NewInt(2000000),
		},
	}
	/*sess.TransactOpts = *auth
	sess.TransactOpts.GasLimit = big.NewInt(4000000) //  Rinkeby block gas limit 6124970 */

	_, err = sess.PopulateCertificate(dataCert)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Populate: Failed to populate blockchain: %v", err),
			580 /*http.StatusInternalServerError*/)
		return
	}
	if (newUserAddr != common.Address{}) {
		_, err := sess.SetOwner(newUserAddr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Rest Populate: Failed to update owner addr: %v", err),
				580 /*http.StatusInternalServerError*/)
			return
		}
		newOwner, err := sess.GetOwner()
		if err != nil {
			http.Error(w, fmt.Sprintf("Rest Populate: Failed to check new owner addr: %v", err),
				580 /*http.StatusInternalServerError*/)
			return
		}
		if newOwner != newUserAddr {
			http.Error(w, fmt.Sprintf("OwnerAddr (%v) does not equal to newUserAddr (%v) despite SetOwner - probably lack of permissions",
				newOwner.String(), newUserAddr.String()), 580 /*http.StatusInternalServerError*/)
			return
		}
	} /*else {
		http.Error(w, "New User addr is null", http.StatusInternalServerError)
		return
	}*/

	//fmt.Printf("Debug Hash Populate: %s, arr:%v \n", hex.EncodeToString(hashCert), []byte(hex.EncodeToString(hashCert)))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(hex.EncodeToString(hashCert)))
}

/*
/create_contract, all params as POST
	Creation of the "empty" CA smart contract:
		a. CA certificate should be added to smart contract through population procedure
		b. the right to execute the smart contract should be changed to the CA account with population procedure as well
	Params:
		ParentAddr: the address of the CA smart contract which is used for creation (it has the bin code)
			This address of this contract should be called at user account CurrentUserAddr
		NewUserAddr - owner is set to this address at the end of the proc. If empty, then new owner is not set
			At the end of the population procedure only the NewUserAddr can modify the smart contract in the future
		CurrentUserAddr: - the user addr to connect to Ethereum. If empty, then set to root user addr
	Returns:
		200 and the smart contract address WITH heading "0x" in the html body in case of success
		Errors (details are in html body):
			480 : Current user does not have rights to execute the creation of the CA certificate
			483 : NewUserAddr is incorrect
			484 : ParentAddr is incorrect
			485 : CurrentUserAddr is incorrect
			580 : Ethereum execution error (out of gas and others)
			581 : Ethereum connection error
			500 : Other error
*/
func rstCreateContract(w http.ResponseWriter, r *http.Request) {
	/*
	   https://vincentserpoul.github.io/post/binding-ethereum-golang/
	   https://ethereum.stackexchange.com/questions/7499/how-are-addresses-created-if-deploying-a-new-bound-contract
	*/
	var parentAddrStr string
	var curUserAddrStr string // !!! presently current user not used - addr=contr.GetOwner used instead
	var newUserAddrStr string

	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		fmt.Printf("CreateContract: No data in multipart form: %v\n", err.Error())
		parentAddrStr = gConfig.ContractHash
	} else {
		strParentAddrArr := r.MultipartForm.Value["ParentAddr"]
		if len(strParentAddrArr) > 0 {
			parentAddrStr = strParentAddrArr[0]
			if common.IsHexAddress(parentAddrStr) == false {
				fmt.Println("Create Contract: Parent address is incorrect")
				http.Error(w, GeneralError{"Rest Create contract: Parent address is incorrect"}.Error(),
					484 /*http.StatusInternalServerError*/)
			}
		} else {
			parentAddrStr = gConfig.ContractHash
		}
	}

	// !!! presently current user not used - addr=contr.GetOwner used instead
	strUserAddrArr := r.MultipartForm.Value["CurrentUserAddr"]
	if len(strUserAddrArr) > 0 {
		curUserAddrStr = strUserAddrArr[0]
		if common.IsHexAddress(curUserAddrStr) == false {
			fmt.Println("Create Contract: Current user address is incorrect")
			http.Error(w, GeneralError{"Rest Create contract: Current user address is incorrect"}.Error(),
				485 /*http.StatusInternalServerError*/)
			return
		}
	}

	strUserAddrArr = r.MultipartForm.Value["NewUserAddr"]
	if len(strUserAddrArr) > 0 {
		newUserAddrStr = strUserAddrArr[0]
		if common.IsHexAddress(newUserAddrStr) == false {
			fmt.Println("Create Contract: New user address is incorrect")
			http.Error(w, "Rest Create contreact: New user address is incorrect", 483 /*http.StatusInternalServerError*/)
			return
		}
	} else {
		http.Error(w, "New user address is not available", 483 /*http.StatusInternalServerError*/)
		return
	}

	client, err := ethclient.Dial(gConfig.IPCpath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Create contract: Failed to connect to the Ethereum client: %v", err),
			581 /*http.StatusInternalServerError*/)
		return
	}

	pkiContr, err := NewLuxUni_PKI(common.HexToAddress(parentAddrStr), client)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Create contract: Failed to instantiate a smart contract: %v", err),
			581 /*http.StatusInternalServerError*/)
		return
	}
	callOpts := &bind.CallOpts{
		Pending: true,
	}
	execUserAddr, err := pkiContr.GetOwner(callOpts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Create contr - failed to get owner addr: ", err),
			581 /*http.StatusInternalServerError*/)
		return
	}
	if execUserAddr != common.HexToAddress(curUserAddrStr) {
		http.Error(w, "Rest Create contract: GetOwner does not correspond to the Current User param",
			480 /*http.StatusInternalServerError*/)
		return
	}

	//keyFile := gConfig.KeyFile
	keyFile, err := FindKeyFile(execUserAddr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Create contract: FindKeyFile: %v. ", err),
			581 /*http.StatusInternalServerError*/)
		return
	}
	key, err := ioutil.ReadFile(gConfig.KeyDir + keyFile)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Create contract: Key File error: %v", err),
			581 /*http.StatusInternalServerError*/)
		return
	}
	//fmt.Printf("DEBUG: Found Ethereum Key File \n")

	auth, err := bind.NewTransactor(strings.NewReader(string(key)), gConfig.Pswd)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Create contract: Failed to create authorized transactor: %v", err),
			581 /*http.StatusInternalServerError*/)
		return
	}
	var trOpts bind.TransactOpts = *auth
	trOpts.GasLimit = big.NewInt(4000000) // 6124970 - block gas limit in Rinkeby
	contrAddr, _ /*contr*/, _, err := DeployLuxUni_PKI(&trOpts, client)
	/*
	   https://stackoverflow.com/questions/40096750/set-status-code-on-http-responsewriter
	*/
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Create contract: CreateContract -- Etherreum error in contract creation: %v", err),
			580 /*http.StatusInternalServerError*/)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(contrAddr.String()))
}

/*
/download_cacert
	Extracting (download) of certificate from CA smart contract
	Params:
		ContrAddr: the address of the CA smart contract
	Returns:
		200 and the smart contract address WITH heading "0x" in the html body in case of success
		Errors (details are in html body):
			484 : ContrAddr is incorrect
			580 : Ethereum execution error (out of gas and others)
			581 : Ethereum connection error
			500 : Other error
	  https://stackoverflow.com/questions/35496233/go-how-to-i-make-download-service
	  https://play.golang.org/p/UMKgI_NLwO
*/
func rstDownloadCaCert(w http.ResponseWriter, r *http.Request) {

	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		fmt.Printf("No change data: Parsing multipart form: %v\n", err.Error())
		return
	}

	if len(r.MultipartForm.Value["ContrAddr"]) == 0 {
		http.Error(w, GeneralError{"No contrAddr is provided"}.Error(),
			484 /*http.StatusInternalServerError*/)
		return
	}
	strContrAddr := r.MultipartForm.Value["ContrAddr"][0]
	if common.IsHexAddress(strContrAddr) == false {
		http.Error(w, GeneralError{"Contract address is incorrect"}.Error(),
			484 /*http.StatusInternalServerError*/)
		return
	}
	contrAddr := common.HexToAddress(strContrAddr)

	/*isCertOK*/ _ /*revokDate*/, _ /*parentAddr*/, _ /*retCaHash*/, _, certData, err :=
		ConfirmHashCAData(contrAddr, nil, true)

	w.Header().Set("Content-Disposition", "attachment; filename=ca.crt.out")
	w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
	http.ServeContent(w, r, "ca.crt.out", time.Now(), bytes.NewReader(certData))
}

/*
/validate_cert, all params as POST
	Parameters:
		Hash or UplFiles (hash is a hex string without a leading "0x")
		UplFiles : uploaded certificate
		ParentAddr: the address of the CA smart contract where the certificate's hash is stored
			If certificate is uploaded through UplFiles, ParentAddr may not be specified
	Returns:
		200 and JSON with the validation results in the html body in case of success
		Errors (details are in html body):
			480 : hash has wrong length or hash is incorrect
			482 : Certificate errors in case it was provided instead of hash
			484 : ParentAddr is incorrect
			580 : Ethereum execution error (out of gas and others)
			581 : Ethereum connection error
			500 : Other error
*/
func rstValidateCert(w http.ResponseWriter, r *http.Request) {

	var parentAddr common.Address
	var isNoUpload bool = false

	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Velidat: No change data: Parsing multipart form: %v", err),
			http.StatusInternalServerError)
		return
	}

	certHash, _ /*fileName*/, dataCert, cerr := UploadFile(w, r, "UplFiles", true)
	if cerr.errCode != 0 {
		if cerr.errCode == 1 {
			isNoUpload = true
			hashArr := r.MultipartForm.Value["Hash"]
			if len(hashArr) == 0 {
				http.Error(w, fmt.Sprintf("Rest Validate: No hashes in request"),
					480 /* http.StatusInternalServerError */)
				return
			}

			hashInt := big.NewInt(0)
			hashInt, _ = hashInt.SetString(hashArr[0], 16) /* tmpInt, err := strconv.Atoi(hashArr[0]); */
			if hashInt == nil {
				http.Error(w, fmt.Sprintf("Rest Validate: Hash string %s is incorrect", hashArr[0]),
					480 /*http.StatusInternalServerError*/)
				return
			}
			certHash = hashInt.Bytes()
		} else {
			http.Error(w, fmt.Sprintf("Rest Validate UplFiles: %v", cerr.Error()),
				482 /*http.StatusInternalServerError*/)
			return
		}
	}

	strParentAddrArr := r.MultipartForm.Value["ParentAddr"]
	if len(strParentAddrArr) > 0 {
		if common.IsHexAddress(strParentAddrArr[0]) == false {
			http.Error(w, fmt.Sprintf("Rest Validate: Parent address as a parameter is incorrect: %v",
				strParentAddrArr[0]),
				484 /*http.StatusInternalServerError*/)
			return
		}
		parentAddr = common.HexToAddress(strParentAddrArr[0])
	}

	if isNoUpload == false {
		var insertAddr common.Address
		_ /*caContrAddr*/, insertAddr, _ /*desc*/, err = ParseCert(dataCert)
		if err != nil {
			http.Error(w, fmt.Sprintf("Rest CERTIFICATE: Parsing error: %v", err),
				482 /*http.StatusInternalServerError*/)
			return
		}
		if (insertAddr == common.Address{}) {
			http.Error(w, "Rest CERTIFICATE: No Parent Address is provided in the Cert",
				482 /*http.StatusInternalServerError*/)
			return
		}
		if (parentAddr != common.Address{}) {
			if insertAddr != parentAddr {
				http.Error(w, "Rest Validate: Address in the certificate does not correspond to the contract address of the Authority (CA)",
					482 /*http.StatusInternalServerError*/)
				return
			}
		} else {
			parentAddr = insertAddr
		}
	}
	if (parentAddr == common.Address{}) {
		http.Error(w, "Rest Validate: Parent Address is not defined",
			484 /*http.StatusInternalServerError*/)
		return
	}

	isCertOK, revokeDate, certPath, iter, err := CheckCertTree(parentAddr, certHash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var jsonResponse JsonValidateResponse
	jsonResponse.RevokeDate = revokeDate
	jsonResponse.IsCertOK = isCertOK
	jsonResponse.Iter = iter
	jsonResponse.CertPath = certPath
	jsonResponse.Status = 0
	bJson, err := json.Marshal(jsonResponse)
	if err != nil {
		http.Error(w, fmt.Sprintf("Rest Validate: Json Marshal:", err), http.StatusInternalServerError)
		return

	}

	w.WriteHeader(http.StatusOK)
	w.Write(bJson)
}

/*
  returns Json string with the path of certificates to the root
          int with the number of iteractions
*/
func CheckCertTree(parentAddr common.Address, userHash []byte) (retIsCertOK bool,
	retRevokeDate time.Time, retCertPath []JsonValidateNode, retIter int, err error) {
	//addr := common.HexToAddress(gConfig.ContractHash);

	var maxIter int = 1000

	iterHash := userHash
	for retIter = 0; retIter < maxIter; retIter++ {
		var jsonNode JsonValidateNode
		jsonNode.ContrAddr = parentAddr.String()
		jsonNode.Hash = fmt.Sprintf("%x", iterHash)

		retIsCertOK, retRevokeDate, parentAddr, iterHash, _, err =
			ConfirmHashCAData(parentAddr, iterHash, false)
		if err != nil {
			return false, time.Time{}, nil, retIter, err
		}

		jsonNode.ParentAddr = parentAddr.String()
		jsonNode.IsCertOK = strconv.FormatBool(retIsCertOK)
		jsonNode.RevokeDate = retRevokeDate.String()
		retCertPath = append(retCertPath, jsonNode)
		if retIsCertOK == false {
			break
		}
		if (parentAddr == common.Address{}) {
			break
		}
		if (retIter >= (maxIter - 1)) && (parentAddr != common.Address{}) {
			return false, time.Time{}, nil, retIter, GeneralError{"MaxIter limit is reached"}
		}
	}
	//bJson, err := json.Marshal(jsonPath)
	return retIsCertOK, retRevokeDate, retCertPath, retIter, nil
}

/*
	Getting results through Events
		the code of event evLuxUni_NewRegDatumReturn(uint256,uint256)
		keccak - web3.sha3("evLuxUni_NewRegDatumReturn(uint256,uint256)") --
				"0x75d1c4f2937517b1233bf95d8f3b4c1d077820b9bc4c5bc28adcd886a3ba7ab6"
				 -- this is the topics in creation of the filter for event logs
*/
func GetEventReturn(dataHash [32]byte, contrAddr common.Address) (result uint64, err error) {

	//var topics [1][1]common.Hash
	//topics[0][0].UnmarshalText(dataHash[:]) //MarshalText(dataHash)

	query := ethereum.FilterQuery{
		FromBlock: nil,
		ToBlock:   nil,
		//Topics:    topics[:][:], //[][]common.Hash
		Addresses: []common.Address{contrAddr}}
	var logs = make(chan types.Log) //, 2)

	client, err := ethclient.Dial(gConfig.IPCpath)
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to connect to the Ethereum client: %v", err)}
	}
	s, err := client.SubscribeFilterLogs(context.TODO(), query, logs)
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to establish Ethereum event filter: %v", err)}
	}

	errChan := s.Err()
	for {
		select {
		case err := <-errChan:
			return 0, GeneralError{fmt.Sprintf("Event Logs subscription error: %v", err)}
		case l := <-logs:
			fmt.Printf("DEBUG Event Data: %x\n", l.Data)
			return ProcEventInteger(l.Data, dataHash)
		}
	}

	//https://blog.golang.org/laws-of-reflection
	/*var tmpRes int64 = -1
	s := reflect.ValueOf(&l).Elem()
	typeOfT := s.Type()

	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		fmt.Printf("%d: %s %s = %v\n", i,
			typeOfT.Field(i).Name, f.Type(), f.Interface())
	}*/

	/*buff, err := CallRPC(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":100}`)
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to call for recent bock Num: %v", err)}
	}
	blockNumStr, err := jsonparser.GetString(buff, "result")
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to parse JSON response for a BlockNum: %v", err)}
	}
	blockNumStr = strings.Replace(blockNumStr, "0x", "", 1)
	log.Printf("blockNumStr2: %v", blockNumStr)
	blockNum, err := strconv.ParseInt(blockNumStr, 16, 64)
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to hex-decode data 1: %v", err)}
	}
	if blockNum > 2 {
		blockNum = blockNum - 2
	}

	requestId, err := strconv.ParseInt(hex.EncodeToString(dataHash[:7]), 16, 64)
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to generate request ID for hash %x, error: %v", dataHash, err)}
	}

	// The code to the event - see the comment to the function
	buff, err = CallRPC(`{"jsonrpc":"2.0","method":"eth_newFilter","params":[{"address": "` + contrAddr.String() +
		`", "topics":["0x75d1c4f2937517b1233bf95d8f3b4c1d077820b9bc4c5bc28adcd886a3ba7ab6"], "fromBlock":"` +
		fmt.Sprintf("0x%x", blockNum) + `","toBlock":"latest"}],"id":"` + fmt.Sprintf("0x%x", requestId) + `"}`)
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to call for newFilter: %v", err)}
	}

	//{"jsonrpc":"2.0","id":31,"result":"0x634ff751e0ee8931b295546c8bda7f9e"}
	//respId, _ := jsonparser.GetInt( buff, "id" )
	hashFilter, err := jsonparser.GetString(buff, "result")
	log.Println("HashFilter: " + hashFilter)
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to parse JSON response for newFilter: %v", err)}
	}

	for iTime := 0; iTime < 2; iTime++ { // this is for the second try in 2(??) seconds

		buff, err = CallRPC(`{"jsonrpc":"2.0","method":"eth_getFilterLogs","params":["` +
			hashFilter + `"],"id":"` + fmt.Sprintf("0x%x", requestId+int64(iTime)+1) + `"}`)
		if err != nil {
			return 0, GeneralError{fmt.Sprintf("Failed to call GetFilterINFO: %v", err)}
		}
		fmt.Println("Event logs: " + string(buff))

		var strData []string
		jsonparser.ArrayEach(buff,
			func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
				str, err := jsonparser.GetString(value, "data")
				if err != nil {
					log.Fatalf("Failed to get string data: %v", err)
				}
				//log.Print(str)
				strData = append(strData, str)
			}, "result")
		if len(strData) == 0 {
			time.Sleep(1 * time.Second)
			fmt.Printf("Return event -- had to wait 1 sec to retrieve event\n")
			jsonparser.ArrayEach(buff,
				func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
					str, err := jsonparser.GetString(value, "data")
					if err != nil {
						log.Fatalf("Failed to get string data 2nd attempt: %v", err)
					}
					//log.Print(str)
					strData = append(strData, str)
				}, "result")
		}
		if len(strData) == 0 {
			return 0, GeneralError{fmt.Sprintf("No data is retrived from the log for hash: %x", dataHash)}
		}

		for iStrData := len(strData) - 1; iStrData >= 0; iStrData = iStrData - 1 {

			tStr := strings.Replace(strData[iStrData], "0x", "", 1)
			if len(tStr) == 32*2*2-1 {
				tStr = "0" + tStr
			}
			if len(tStr) != 32*2*2 {
				return 0, GeneralError{fmt.Sprintf("Length of the data string is not valid: %v, dataHash: %x",
					len(tStr), dataHash)}
			}

			eventHash, err := hex.DecodeString(tStr[:32*2])
			if err != nil {
				return 0, GeneralError{fmt.Sprintf("Did not manage to parse event hash: %v", err)}
			}
			eventReturn, err := strconv.ParseInt(tStr[(32*2*2-64/8*2):32*2*2], 16, 64)
			if err != nil {
				return 0, GeneralError{fmt.Sprintf("Did not manage to parse event hash: %v", err)}
			}

			if bytes.Equal(eventHash, dataHash[:]) == true {
				tmpRes = eventReturn
				break
			}
		}
		if tmpRes == -1 && iTime == 1 {
			time.Sleep(2 * time.Second) // wait 2 seconds if no results for this hash were found
			fmt.Printf("Return event -- had to wait 2 sec for the second try\n")
		} else {
			break // break if some reaults for a given hash were found
		}
	}

	buff, err = CallRPC(`{"jsonrpc":"2.0","method":"eth_uninstallFilter","params":["` + hashFilter + `"],"id":"` +
		fmt.Sprintf("0x%x", requestId) + `"}`)
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to receive response for UninstallFilter: %v", err)}
	}

	uninst, _, _, err := jsonparser.Get(buff, "result")
	if err != nil {
		return 0, GeneralError{fmt.Sprintf("Failed to parse Json for UninstallFilter: %v", err)}
	}
	if string(uninst) != "true" {
		fmt.Println("Warning - the event filter uninstall was unsuccessful")
	}
	if tmpRes == -1 {
		return 0, GeneralError{fmt.Sprintf("The return value corresponding to datahash not found: %x", dataHash)}
	}
	result = uint64(tmpRes)
	return result, nil*/
}

func ProcEventInteger(evData []byte, dataHash [32]byte) (result uint64, err error) {

	if len(evData) != 32*2 {
		return 0, GeneralError{fmt.Sprintf("Length of the data string is not valid: %v, dataString: %x, dataHash: %x",
			len(evData), evData, dataHash)}
	}

	eventHash := evData[:32]

	if bytes.Equal(eventHash, dataHash[:]) == true {
		eventReturn := binary.BigEndian.Uint64(evData[(32*2 - 64/8) : 32*2])
		return eventReturn, nil
	}
	return 0, GeneralError{fmt.Sprintf("Hash %x is not found in data string %x",
		dataHash, evData)}
}

func CallRPC(query string) ([]byte, error) {
	body := strings.NewReader(query)
	req, err := http.NewRequest("POST", gConfig.EthereumRpcUrl+":"+strconv.Itoa(gConfig.EthereumRpcPort), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}
