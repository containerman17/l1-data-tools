GET
/v1/networks/{network}/addresses:listChainIds
Get chain interactions for addresses


Returns Primary Network chains that each address has touched in the form of an address mapped array. If an address has had any on-chain interaction for a chain, that chain's chain id will be returned.

Parameters
Cancel
Name	Description
addresses *
string
(query)
A comma separated list of X-Chain or P-Chain wallet addresses, starting with "avax"/"fuji", "P-avax"/"P-fuji" or "X-avax"/"X-fuji".


network *
string
(path)
Either mainnet or testnet/fuji.


Servers
These operation-level options override the global server options.

Execute
Clear
Responses
Curl

curl -X 'GET' \
  'https://data-api.avax.network/v1/networks/mainnet/addresses:listChainIds?addresses=avax1h2ccj9f5ay5acl6tyn9mwmw32p8wref8vl8ctg' \
  -H 'accept: application/json'
Request URL
https://data-api.avax.network/v1/networks/mainnet/addresses:listChainIds?addresses=avax1h2ccj9f5ay5acl6tyn9mwmw32p8wref8vl8ctg
Server response
Code	Details
200	
Response body
Download
{
  "addresses": [
    {
      "address": "avax1h2ccj9f5ay5acl6tyn9mwmw32p8wref8vl8ctg",
      "blockchainIds": [
        "11111111111111111111111111111111LpoYY"
      ]
    }
  ]
}