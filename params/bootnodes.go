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

package params

import "github.com/UltronGlow/UltronGlow-Origin/common"

// MainnetBootnodes are the enode URLs of the P2P bootstrap nodes running on
// the main utg network.
var MainnetBootnodes = []string{
	// utg Foundation Go Bootnodes
	"enode://617ff1e65455373593e08f08bb44e1c3e5dc3736e824870e5e2ab42b501610570b7bbaeb9621af0df04919cd0d4782924465ca39c97a811be0f3600dfe1d0fff@65.19.174.250:30313",
	"enode://9591316492a851ec7631f46f56633172804d7d5f81a56b98b3e378cb48fc01844b690d923fd55a6b5d5bbed0e96fc65da967f1752d1d5f4b30b76aa118af4a1f@65.19.174.250:30314",
	"enode://42255d9d3bd5b4eb9c19ae0f6a6f03dfe66eec89c69fa7936fcb340ad9b2eca7ad51b2ceee67373e7d82538e0f506df6d9deccc4edfdf7d7899896af42b237ca@65.19.174.250:30315",
	"enode://a0ca9ecb3604178be9ef738bababd0c2534fae6f5b49db2c34c60a86e371a8605a5059c1d3770a11962098edbe8f1ff8f6b84a34f216e60451d94b935185550a@3.11.146.224:30311",
	"enode://d76b3c678b8d7fcb547546fc6b54ae85a08422dac849aec372b9cf476ec562ca8ca238554541fe39e6348de85f7ac50954fc2870f50d12254c1fa64978bdb7c6@157.175.241.106:30311",
	"enode://673fa21525f9fa4196d4397f0e9c684b7a9a0a9988dd1c9be666662548337a1c63b521b01ade4596315a9522767dc0fd4ef0d1512041b1a99dad0550b6495344@15.228.2.99:30311",
	"enode://53ccd4ac395851b58fb62062f8878c2a76d331fb55a0b23bdda65ec0d612ec885bef33cc1587c375f9425460668c7735f1d886b25838421e38be38ba3d23b9b4@52.220.117.106:30311",
	"enode://d76ba3ab519c9310e93f5b84574552a19cc7b29fc521dbe2f74cf7a1bcd37ffdb490e5a1b8b370024249546db469f72bf1f667c9a2647df20f6d257a8a4dabd0@54.253.52.162:30311",
	"enode://002d2f0531bb308d5478fb853ad7509a8aa337ffb71e9a8b957dc8a0d9f0ff2d034da15ae9d140f84cad797d8c3ed72c0c36b2b42de4832c7f16dd0e9824b7d6@54.193.66.199:30311",
	"enode://a0829340110d09845a8be8150a976d3e8ca8ad30feafc8a83088b06aace0e23d2ad6ab7529d0c95d14fadbe0b2972ae2ab6639e9e6ee43de765ccc68748390b8@52.204.48.102:30311",
	"enode://333c692d3fd33a7e31f91bd2b6412f87a580c57540a89f929103e90f48c9729c9f7f1348cb388fa38d158caf97e6d75af8b414374d85fcb17c5f2cd3052d5a8f@35.75.161.172:30311",
	"enode://da125a348cc7c3f7d15f0e582ad17e3bf755741c5d56731a7e0dc6ff8df6a61739987ecf0fa942df122fee12def086ddb16352ee50249e26ca6d63d698be722b@43.206.139.49:30303",
	"enode://838cf9c1f19b449130e6e63f9f51457c87276ce685739f9bef774afd4d9b19776840a67ef751155bcc30fd44e5a807fc895eee095304844311f71c12445426e1@3.8.212.51:30303",
	"enode://77209675949af361199eb702d3fac87158b78b758c4692950aa048fd5857ea3582083ae2c18b566a06da79b0ee4054f7646c9f8d14f13ccbdcebd05fa81fe09c@13.38.30.109:30303",
	"enode://66a9d03f4b6a68f2334ee46a2790d8bd09c788400056cd5512f4290c20c731327ea214023a33c7196f62e51ef0ae21dbf9587be69d6861ce6ec91f0f80e1d7f4@54.233.222.55:30303",
	"enode://7e5093a678fab8f1583535b87c1ae18d185e26efafa539dc2ab217b2d9ff033f99aa6b34b46595c7e1b11e5062d07d7faf8ac3eee70d34236d802b04f82a6241@52.221.253.44:30303",
	"enode://bd6e1174161c54e64e3a91749db6f0a8b7c4cb2b036397590db674d88a8c1748c1506e878b07a3fe6047702829ba0af1eec2bad4a39db4d9291721649420e83d@3.26.255.47:30303",
	"enode://457e77db45079dad53e23d5581850381f529fceb8e287bc60f63458fdc8a785adc7abf2d15d6db098c745798a1db5caa4c1b94259f44f57db2de4f8968cd2ba2@54.193.242.25:30303",
	"enode://51c3c4a491466d818529142807469da494f76d60e7aa426cf7e1bd35480aab3e7f7b9d279b21510e585489fa0b8b7611068300f5fda12fc03295b2f7f353f24d@3.80.201.230:30303",
}

// TestnetBootnodes are the enode URLs of the P2P bootstrap nodes running on the
// Testnet test network.
var TestnetBootnodes = []string{
	"enode://2ffed1bb6b475259c1448dc93b639569886999e51ade144451877a706d2a9b71eff8eb067d289fde48ba4807370034d851553746fac8816af27f5a922703e2e4@127.0.0.1:30311",
}

var V5Bootnodes = []string{
}

const dnsPrefix = "enrtree://AKA3AM6LPBYEUDMVNU3BSVQJ5AD45Y7YPOHJLEF6W26QOE4VTUDPE@"

// KnownDNSNetwork returns the address of a public DNS-based node list for the given
// genesis hash and protocol. See https://github.com/ethereum/discv4-dns-lists for more
// information.
func KnownDNSNetwork(genesis common.Hash, protocol string) string {
	var net string
	switch genesis {
	case MainnetGenesisHash:
		net = "mainnet"
	case TestnetGenesisHash:
		net = "testnet"
	default:
		return ""
	}
	return dnsPrefix + protocol + "." + net + ".ethdisco.net"
}