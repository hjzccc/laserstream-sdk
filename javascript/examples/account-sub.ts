import { subscribe, CommitmentLevel, LaserstreamConfig, SubscribeRequest } from '../src';

async function main() {
  const subscriptionRequest = {
    accounts: {
        accountSubscribe: {
            account: [],
            owner: ["pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA", "LBUZKhRxPF3XUpBCjp4YzTKgLccjZhTSDM9YuVaPwxo", "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"],
            filters: []
        }
    },
    accountsDataSlice: [],
    commitment: CommitmentLevel.PROCESSED,
    slots: {},
    transactions: {},
    transactionsStatus: {},
    blocks: {},
    blocksMeta: {},
    entry: {}
};

const config = {
  apiKey: '',
  endpoint: ''
}

  await subscribe(config, subscriptionRequest, async (data) => {

    console.log(data)
  }, async (error) => {
    console.error(error);
  });
}

main().catch(console.error);