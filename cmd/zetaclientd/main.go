package main

import (
	"flag"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"github.com/zeta-chain/zetacore/cmd"
	"github.com/zeta-chain/zetacore/common"
	"github.com/zeta-chain/zetacore/common/cosmos"
	mc "github.com/zeta-chain/zetacore/zetaclient"
	"github.com/zeta-chain/zetacore/zetaclient/config"
	metrics2 "github.com/zeta-chain/zetacore/zetaclient/metrics"
	//mcconfig "github.com/Meta-Protocol/zetacore/metaclient/config"
	"github.com/cosmos/cosmos-sdk/types"
	//"github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p-peerstore/addr"
	maddr "github.com/multiformats/go-multiaddr"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	fmt.Printf("zetacore commit hash %s version %s build time %s \n", common.CommitHash, common.Version, common.BuildTime)

	var valKeyName = flag.String("val", "alice", "validator name")
	var peer = flag.String("peer", "", "peer address, e.g. /dns/tss1/tcp/6668/ipfs/16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp")
	flag.Parse()

	var peers addr.AddrList
	fmt.Println("peer", *peer)
	if *peer != "" {
		address, err := maddr.NewMultiaddr(*peer)
		if err != nil {
			log.Error().Err(err).Msg("NewMultiaddr error")
			return
		}
		peers = append(peers, address)
	}

	fmt.Println("multi-node client")
	start(*valKeyName, peers)
}

func SetupConfigForTest() {
	config := cosmos.GetConfig()
	config.SetBech32PrefixForAccount(cmd.Bech32PrefixAccAddr, cmd.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(cmd.Bech32PrefixValAddr, cmd.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(cmd.Bech32PrefixConsAddr, cmd.Bech32PrefixConsPub)
	//config.SetCoinType(cmd.MetaChainCoinType)
	config.SetFullFundraiserPath(cmd.ZetaChainHDPath)
	types.SetCoinDenomRegex(func() string {
		return cmd.DenomRegex
	})

	rand.Seed(time.Now().UnixNano())

}

func start(validatorName string, peers addr.AddrList) {
	SetupConfigForTest() // setup meta-prefix
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	chainIP := os.Getenv("CHAIN_IP")
	if chainIP == "" {
		chainIP = "127.0.0.1"
	}

	ethEndPoint := os.Getenv("GOERLI_ENDPOINT")
	if ethEndPoint != "" {
		config.GOERLI_ENDPOINT = ethEndPoint
		log.Info().Msgf("GOERLI_ENDPOINT: %s", ethEndPoint)
	}
	bscEndPoint := os.Getenv("BSCTESTNET_ENDPOINT")
	if bscEndPoint != "" {
		config.BSCTESTNET_ENDPOINT = bscEndPoint
		log.Info().Msgf("BSCTESTNET_ENDPOINT: %s", bscEndPoint)
	}
	polygonEndPoint := os.Getenv("MUMBAI_ENDPOINT")
	if polygonEndPoint != "" {
		config.MUMBAI_ENDPOINT = polygonEndPoint
		log.Info().Msgf("MUMBAI_ENDPOINT: %s", polygonEndPoint)
	}
	ropstenEndPoint := os.Getenv("ROPSTEN_ENDPOINT")
	if ropstenEndPoint != "" {
		config.ROPSTEN_ENDPOINT = ropstenEndPoint
		log.Info().Msgf("ROPSTEN_ENDPOINT: %s", ropstenEndPoint)
	}

	ethMpiAddress := os.Getenv("GOERLI_MPI_ADDRESS")
	if ethMpiAddress != "" {
		config.Chains[common.GoerliChain.String()].ConnectorContractAddress = ethMpiAddress
		log.Info().Msgf("ETH_MPI_ADDRESS: %s", ethMpiAddress)
	}
	bscMpiAddress := os.Getenv("BSCTESTNET_MPI_ADDRESS")
	if bscMpiAddress != "" {
		config.Chains[common.BSCTestnetChain.String()].ConnectorContractAddress = bscMpiAddress
		log.Info().Msgf("BSC_MPI_ADDRESS: %s", bscMpiAddress)
	}
	polygonMpiAddress := os.Getenv("MUMBAI_MPI_ADDRESS")
	if polygonMpiAddress != "" {
		config.Chains[common.MumbaiChain.String()].ConnectorContractAddress = polygonMpiAddress
		log.Info().Msgf("polygonMpiAddress: %s", polygonMpiAddress)
	}
	ropstenMpiAddress := os.Getenv("ROPSTEN_MPI_ADDRESS")
	if ropstenMpiAddress != "" {
		config.Chains[common.RopstenChain.String()].ConnectorContractAddress = ropstenMpiAddress
		log.Info().Msgf("ropstenMpiAddress: %s", ropstenMpiAddress)
	}

	// wait until zetacore is up
	log.Info().Msg("Waiting for ZetaCore to open 9090 port...")
	for {
		_, err := grpc.Dial(
			fmt.Sprintf("%s:9090", chainIP),
			grpc.WithInsecure(),
		)
		if err != nil {
			log.Warn().Err(err).Msg("grpc dial fail")
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}
	log.Info().Msgf("ZetaCore to open 9090 port...")

	// setup 2 metabridges
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Err(err).Msg("UserHomeDir error")
		return
	}
	chainHomeFoler := filepath.Join(homeDir, ".zetacore")

	// first signer & bridge
	signerName := validatorName
	signerPass := "password"
	bridge1, done := CreateMetaBridge(chainHomeFoler, signerName, signerPass)
	if done {
		return
	}

	key, err := bridge1.GetKeys().GetPrivateKey()
	if err != nil {
		log.Error().Err(err).Msg("GetKeys GetPrivateKey error:")
	}
	if len(key.Bytes()) != 32 {
		log.Error().Msgf("key bytes len %d != 32", len(key.Bytes()))
		return
	}
	var priKey secp256k1.PrivKey
	priKey = key.Bytes()[:32]

	log.Info().Msgf("NewTSS: with peer pubkey %s", key.PubKey())
	tss, err := mc.NewTSS(peers, priKey)
	if err != nil {
		log.Error().Err(err).Msg("NewTSS error")
		return
	}

	_, err = bridge1.SetTSS(common.GoerliChain, tss.Address().Hex(), tss.PubkeyInBech32)
	if err != nil {
		log.Error().Err(err).Msgf("SetTSS fail %s", common.GoerliChain)
	}
	_, err = bridge1.SetTSS(common.BSCTestnetChain, tss.Address().Hex(), tss.PubkeyInBech32)
	if err != nil {
		log.Error().Err(err).Msgf("SetTSS fail %s", common.BSCTestnetChain)
	}
	_, err = bridge1.SetTSS(common.MumbaiChain, tss.Address().Hex(), tss.PubkeyInBech32)
	if err != nil {
		log.Error().Err(err).Msgf("SetTSS fail %s", common.MumbaiChain)
	}
	_, err = bridge1.SetTSS(common.RopstenChain, tss.Address().Hex(), tss.PubkeyInBech32)
	if err != nil {
		log.Error().Err(err).Msgf("SetTSS fail %s", common.RopstenChain)
	}

	signerMap1, err := CreateSignerMap(tss)
	if err != nil {
		log.Error().Err(err).Msg("CreateSignerMap")
		return
	}

	metrics, err := metrics2.NewMetrics()
	if err != nil {
		log.Error().Err(err).Msg("NewMetrics")
		return
	}
	metrics.Start()

	userDir, _ := os.UserHomeDir()
	dbpath := filepath.Join(userDir, ".zetaclient/chainobserver")
	chainClientMap1, err := CreateChainClientMap(bridge1, tss, dbpath, metrics)
	if err != nil {
		log.Err(err).Msg("CreateSignerMap")
		return
	}

	log.Info().Msg("starting zetacore observer...")
	mo1 := mc.NewCoreObserver(bridge1, signerMap1, *chainClientMap1, metrics, tss)

	mo1.MonitorCore()

	// report node key
	// convert key.PubKey() [cosmos-sdk/crypto/PubKey] to bech32?
	s, err := cosmos.Bech32ifyPubKey(cosmos.Bech32PubKeyTypeAccPub, key.PubKey())
	if err != nil {
		log.Error().Err(err).Msgf("Bech32ifyPubKey fail in main")
	}
	log.Info().Msgf("GetPrivateKey for pubkey bech32 %s", s)

	pubkey, err := common.NewPubKey(s)
	if err != nil {
		log.Error().Err(err).Msgf("NewPubKey error from string %s:", key.PubKey().String())
	}
	pubkeyset := common.PubKeySet{
		Secp256k1: pubkey,
		Ed25519:   "",
	}
	conskey := ""
	ztx, err := bridge1.SetNodeKey(pubkeyset, conskey)
	log.Info().Msgf("SetNodeKey: %s by node %s zeta tx %s", pubkeyset.Secp256k1.String(), conskey, ztx)
	if err != nil {
		log.Error().Err(err).Msgf("SetNodeKey error")
	}

	// report TSS address nonce on ETHish chains
	err = (*chainClientMap1)[common.GoerliChain].PostNonceIfNotRecorded()
	if err != nil {
		log.Error().Err(err).Msgf("PostNonceIfNotRecorded fail %s", common.GoerliChain)
	}
	err = (*chainClientMap1)[common.BSCTestnetChain].PostNonceIfNotRecorded()
	if err != nil {
		log.Error().Err(err).Msgf("PostNonceIfNotRecorded fail %s", common.BSCTestnetChain)
	}
	err = (*chainClientMap1)[common.MumbaiChain].PostNonceIfNotRecorded()
	if err != nil {
		log.Error().Err(err).Msgf("PostNonceIfNotRecorded fail %s", common.MumbaiChain)
	}
	err = (*chainClientMap1)[common.RopstenChain].PostNonceIfNotRecorded()
	if err != nil {
		log.Error().Err(err).Msgf("PostNonceIfNotRecorded fail %s", common.RopstenChain)
	}

	// printout debug info from SIGUSR1
	// trigger by $ kill -SIGUSR1 <PID of zetaclient>
	usr := make(chan os.Signal, 1)
	signal.Notify(usr, syscall.SIGUSR1)
	go func() {
		for {
			<-usr
			fmt.Printf("Last blocks:\n")
			fmt.Printf("ETH     %d:\n", (*chainClientMap1)[common.GoerliChain].LastBlock)
			fmt.Printf("BSC     %d:\n", (*chainClientMap1)[common.BSCTestnetChain].LastBlock)
			fmt.Printf("POLYGON %d:\n", (*chainClientMap1)[common.MumbaiChain].LastBlock)
			fmt.Printf("ROPSTEN %d:\n", (*chainClientMap1)[common.RopstenChain].LastBlock)

		}
	}()

	// wait....
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Info().Msg("stop signal received")

	(*chainClientMap1)[common.ETHChain].Stop()
	(*chainClientMap1)[common.BSCChain].Stop()
	(*chainClientMap1)[common.POLYGONChain].Stop()
	(*chainClientMap1)[common.RopstenChain].Stop()
}
