import { subscribe, CommitmentLevel, LaserstreamConfig } from '../src';
import Client from '@triton-one/yellowstone-grpc';

import bs58 from 'bs58';

type Seen = { slotA?: string; slotB?: string };
const seen = new Map<string, Seen>();

async function main() {

  const PUMP = 'pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA';

  // Primary stream (under test)
  const config: LaserstreamConfig = {
    apiKey: '',
    endpoint: ''
  };


  const subscriptionRequest: any = {
    transactions: {
      client: {
        accountInclude: [PUMP],
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
  };

  //Yellowstone node for comparing with Laserstream
  const referenceConfig = {
    endpoint: '',
    xToken: ''
  } as const;


  const gotA = new Set<string>(); // primary signatures
  const gotB = new Set<string>(); // reference signatures
  let newA = 0;
  let newB = 0;


  const INTEGRITY_CHECK_INTERVAL_MS = 30_000; // 30 seconds
  setInterval(async () => {
    const missed = new Set<string>();
    for (const sig of gotB) {
      if (!gotA.has(sig)) missed.add(sig);
    }

    const now = new Date().toISOString();
    console.log(`[${now}] A:+${newA}  B:+${newB}  missed:${missed.size}`);

    if (missed.size) {
      for (const sig of missed) console.error(`MISSED ${sig}`);
    }

    // reset counters (keep global sets for history but clear new counters)
    newA = 0;
    newB = 0;
  }, INTEGRITY_CHECK_INTERVAL_MS);

  console.log('Starting transaction integrity test…');

  // Helper: narrow update to transaction variant – we work with `any` here to avoid heavy protobuf typings(idk how to do this better)
  function isTransactionUpdate(update: any): update is { transaction: any } {
    return typeof (update as any).transaction === 'object' && (update as any).transaction !== null;
  }

  // Helper: reliably extract a base-58 signature string from a transaction update
  function extractSigAndSlot(update: any): {sig: string | null; slot: string | null} {
    if (!isTransactionUpdate(update)) return {sig: null, slot: null} as any;

    const info: any | undefined = (update.transaction as any)?.transaction;

    if (!info) return {sig: null, slot: null} as any;

    const rawSig: Uint8Array | undefined = (info.signature ?? info.transaction?.signature) as Uint8Array | undefined;
    if (!rawSig) return {sig: null, slot: null} as any;

    const sigStr = bs58.encode(Buffer.from(rawSig));
    const slotStr = (update.transaction as any)?.slot ?? null;
    return {sig: sigStr, slot: slotStr};
  }

  const statusMap = new Map<string, {slotPrimary?: string; slotRef?: string}>();

  function maybePrint(sig: string) {
    const entry = statusMap.get(sig);
    if (entry && entry.slotPrimary && entry.slotRef) {
      console.log(`MATCH ${sig}  LS_slot=${entry.slotPrimary}  YS_slot=${entry.slotRef}`);
      statusMap.delete(sig);
    }
  }

  // ---------- helper to start a stream ----------
  const startPrimaryStream = async (
    cfg: LaserstreamConfig,
    onSig: (sig: string) => void,
    label: string
  ) => {
    await subscribe(
      cfg,
      subscriptionRequest,
      (u) => {
        const {sig, slot} = extractSigAndSlot(u);
        if (!sig) return;
        onSig(sig);
        const entry = statusMap.get(sig) || {};
        entry.slotPrimary = slot ?? 'unknown';
        statusMap.set(sig, entry);
        maybePrint(sig);
      },
      (err) => {
        console.error(`${label} error:`, err);
      }
    );
  };

  // Reference stream using raw Yellowstone gRPC client
  const startReferenceStream = async () => {
    const client = new Client(referenceConfig.endpoint, referenceConfig.xToken, {
      "grpc.max_receive_message_length": 64 * 1024 * 1024,
    });

    const stream = await client.subscribe();

    // send the request
    await new Promise<void>((resolve, reject) => {
      stream.write(subscriptionRequest, (err: Error | null | undefined) => {
        if (err == null) resolve();
        else reject(err);
      });
    });

    stream.on('data', (u: any) => {
      const {sig, slot} = extractSigAndSlot(u);
      if (!sig) return;
      gotB.add(sig);
      newB += 1;

      const entry = statusMap.get(sig) || {};
      entry.slotRef = slot ?? 'unknown';
      statusMap.set(sig, entry);
      maybePrint(sig);
    });

    stream.on('error', (e: Error) => console.error('REFERENCE stream error:', e));
  };

  // Start both streams concurrently
  await Promise.all([
    startPrimaryStream(config, (sig) => {
      gotA.add(sig);
      newA += 1;
    }, 'PRIMARY'),
    startReferenceStream(),
  ]);

  // Keep the script alive indefinitely so we can continue to receive updates.
  await new Promise(() => {});
}

main().catch((err) => {
  console.error('Unhandled error in main:', err);
  process.exit(1);
});