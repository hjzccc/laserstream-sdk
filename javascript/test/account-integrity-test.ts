import { subscribe, CommitmentLevel, LaserstreamConfig } from '../src';
import Client from '@triton-one/yellowstone-grpc';
import bs58 from 'bs58';

const ACCOUNTS = [
  'pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA', // Pump AMM program
  'TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA', // SPL Token program
];

const primaryCfg: LaserstreamConfig = {
  apiKey: '',
  endpoint: '',
};

//Yellowstone node for comparing with Laserstream
const referenceCfg = {
  endpoint: '',
  xToken: '',
} as const;

const subscribeReq: any = {
  accounts: {
    tracked: {
      account: ACCOUNTS,
      owner: [],
      filters: [],
    },
  },
  accountsDataSlice: [],
  commitment: CommitmentLevel.CONFIRMED,
  slots: {},
  transactions: {},
  transactionsStatus: {},
  blocks: {},
  blocksMeta: {},
  entry: {},
};

// ---------- STATE ----------
interface Seen { slotA?: string; slotB?: string }
const state = new Map<string, Seen>();
let newA = 0;
let newB = 0;

function isAccountUpdate(u: any): u is { account: any } {
  return typeof u.account === 'object' && u.account !== null;
}

function extractKeyAndSlot(u: any): { key: string | null; slot: string | null } {
  if (!isAccountUpdate(u)) return { key: null, slot: null };
  const info = u.account.account;
  if (!info) return { key: null, slot: null };
  const keyBuf: Uint8Array | undefined = info.pubkey as Uint8Array | undefined;
  if (!keyBuf) return { key: null, slot: null };
  return { key: bs58.encode(Buffer.from(keyBuf)), slot: u.account.slot ?? null };
}

function maybePrint(key: string) {
  const rec = state.get(key);
  if (rec?.slotA && rec?.slotB) {
    console.log(`MATCH acct=${key}  LS_slot=${rec.slotA}  YS_slot=${rec.slotB}`);
    state.delete(key);
  }
}

async function startPrimary() {
  await subscribe(
    primaryCfg,
    subscribeReq,
    (u) => {
      const { key, slot } = extractKeyAndSlot(u);
      if (!key) return;
      const entry = state.get(key) || {};
      entry.slotA = slot ?? 'unknown';
      state.set(key, entry);
      newA += 1;
      maybePrint(key);
    },
    (err) => console.error('PRIMARY error:', err),
  );
}

async function startReference() {
  const client = new Client(referenceCfg.endpoint, referenceCfg.xToken, {
    'grpc.max_receive_message_length': 64 * 1024 * 1024,
  });
  const stream = await client.subscribe();
  await new Promise<void>((resolve, reject) => {
    stream.write(subscribeReq, (e: any) => (e ? reject(e) : resolve()));
  });
  stream.on('data', (u: any) => {
    const { key, slot } = extractKeyAndSlot(u);
    if (!key) return;
    const entry = state.get(key) || {};
    entry.slotB = slot ?? 'unknown';
    state.set(key, entry);
    newB += 1;
    maybePrint(key);
  });
  stream.on('error', (e: Error) => console.error('REFERENCE error:', e));
}

const INTERVAL = 30_000;
setInterval(() => {
  const now = new Date().toISOString();
  console.log(`[${now}] primary+${newA}  reference+${newB}`);
  newA = 0;
  newB = 0;
}, INTERVAL);

(async () => {
  console.log('Starting account integrity test…');
  await Promise.all([startPrimary(), startReference()]);
  await new Promise(() => {});
})(); 