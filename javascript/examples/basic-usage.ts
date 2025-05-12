import { subscribe, CommitmentLevel, LaserstreamConfig, SubscribeRequest } from '../src';

async function main() {
  const subscriptionRequest: SubscribeRequest = {
    transactions: {
      client: {
        accountInclude: ['TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA'], // Subscribe to all confirmed token transactions
        accountExclude: [],
        accountRequired: [],
        vote: false,
        failed: false
      }
    },
    commitment: CommitmentLevel.CONFIRMED,
    accounts: {},
    slots: {},
    transactionsStatus: {},
    blocks: {},
    blocksMeta: {},
    entry: {},
    accountsDataSlice: [],
  }

  const config: LaserstreamConfig = {
    apiKey: '',
    endpoint: '',
  }

  await subscribe(config, subscriptionRequest, async (data) => {
    
    console.log(data);

  }, async (error) => {
    console.error(error);
  });
}

main().catch(console.error);