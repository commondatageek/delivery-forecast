// Registry of native result views: command id → (resultEvent, command) => Node.
// Commands without an entry fall back to the "Output" content.
import { simItems, simDays, simProbability, simBacktest } from './sim.js';
import { aging, cfd, history, count, check } from './flow.js';

export const views = {
  'sim-items': simItems,
  'sim-days': simDays,
  'sim-probability': simProbability,
  'sim-backtest': simBacktest,
  history,
  cfd,
  aging,
  count,
  check,
};
