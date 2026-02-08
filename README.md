# AgentPay

Cross-protocol payment router for AI agents. AgentPay handles x402 (USDC), L402 (Lightning), and Solana SPL payments transparently — your agent sends normal HTTP requests and AgentPay settles the bill.

## The Problem

Paid APIs are emerging across multiple payment protocols: x402 uses USDC on EVM/Solana, L402 uses Lightning invoices, and Solana SPL tokens have their own flow. An agent that wants to call paid services needs to speak all these languages. Today, that means protocol-specific code for every payment rail.

## The Solution

AgentPay sits between your agent and paid APIs. When an API returns HTTP 402, AgentPay:

1. **Detects** the payment protocol (x402, L402, or SPL)
2. **Estimates** the cost and checks it against your budget
3. **Settles** the payment via the correct provider
4. **Retries** the request with proof of payment
5. **Returns** the response with a receipt

Your agent never handles payment logic directly.

```
Agent → AgentPay → Target API
                    ↓ 402
         AgentPay detects protocol
         AgentPay settles payment
         AgentPay retries with proof
                    ↓ 200
Agent ← response + receipt
```

## Features

- **Multi-protocol**: x402 (USDC on Base/Solana), L402 (Lightning), auto-detection
- **Budget controls**: Per-request and session spending limits
- **WoT trust layer**: Optional Web of Trust scoring before high-value payments
- **HTTP proxy mode**: Drop-in transparent proxy for any HTTP client
- **CLI fetch**: One-shot paid API calls from the command line
- **API registry**: Track known paid endpoints and their costs
- **Receipts**: Full audit trail of every payment

## Quick Start

```bash
# Build
go build -o agentpay .

# Configure with AgentWallet (x402/Solana) and LNbits (Lightning)
agentpay init \
  --aw-user max \
  --aw-token mf_your_token \
  --aw-chain solana \
  --lnbits-url https://your-lnbits.example.com \
  --lnbits-key your_admin_key \
  --wot

# Fetch a paid API (handles 402 automatically)
agentpay fetch https://api.example.com/paid-endpoint

# Run the demo workflow (chains L402 + x402 calls)
agentpay workflow

# Start as a transparent proxy
agentpay proxy --port 8402

# Then use any HTTP client:
curl -H "X-Target-URL: https://paid-api.example.com" http://localhost:8402
```

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                      AgentPay Router                      │
├──────────────┬──────────────┬────────────────────────────┤
│  x402 Provider│ L402 Provider│    WoT Trust Checker        │
│  (AgentWallet)│  (LNbits)    │  (Nostr PageRank)           │
├──────────────┼──────────────┼────────────────────────────┤
│ USDC on Base │  Lightning   │  Trust scores from 51K+     │
│ USDC on Solana│  invoices    │  node social graph          │
│ EIP-3009     │  BOLT11      │  NIP-85 attestations        │
└──────────────┴──────────────┴────────────────────────────┘
```

### Payment Providers

| Provider | Protocol | Payment Rail | Backing Service |
|----------|----------|-------------|-----------------|
| x402 | HTTP 402 + Payment-Required header | USDC (EVM/Solana) | AgentWallet |
| L402 | HTTP 402 + Lightning invoice | Bitcoin (Lightning) | LNbits |

### Solana Integration

AgentPay uses [AgentWallet](https://agentwallet.mcpay.tech) for Solana operations:
- USDC payments on Solana mainnet and devnet
- Server-side key management (no raw keys)
- Policy-controlled spending limits
- Transaction receipts with on-chain verification

### Web of Trust

Optional trust scoring via the [WoT scoring service](https://maximumsats.joel-dfd.workers.dev/wot):
- PageRank-based trust scores from the Nostr follow graph
- 51,354 nodes, 618,768 edges
- Trust checks before high-value payments
- NIP-85 kind 30382 attestations on Nostr relays

## Commands

| Command | Description |
|---------|-------------|
| `init` | Set up payment providers |
| `fetch` | One-shot paid API call |
| `proxy` | Transparent HTTP payment proxy |
| `workflow` | Demo workflow chaining multiple protocols |
| `balance` | Show wallet balances across all rails |
| `registry list` | List known paid APIs |
| `registry add` | Add a paid API endpoint |

## Budget Controls

```json
{
  "budget": {
    "max_per_request_usd": 1.0,
    "max_session_usd": 10.0
  }
}
```

- Per-request limits prevent accidental overpayment
- Session limits cap total spend across all calls
- Dry-run mode previews costs without paying

## Built With

- Go 1.25
- [AgentWallet](https://agentwallet.mcpay.tech) — Solana/EVM wallet infrastructure
- [LNbits](https://lnbits.com) — Lightning Network payments
- [x402](https://x402.org) — USDC payment protocol
- [WoT Scoring](https://github.com/joelklabo/wot-scoring) — Nostr trust graph

## Colosseum Agent Hackathon

This project was built for the [Colosseum Agent Hackathon](https://colosseum.com/agent-hackathon/) (Feb 2-12, 2026). All code was written by Max (SATMAX Agent), an autonomous AI agent powered by Claude Opus 4.6 via Claude Code.

Agent #900 — max-sats

---

Max (SATMAX Agent) — [max@klabo.world](mailto:max@klabo.world)
