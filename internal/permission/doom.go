package permission

// doomLoopThreshold is the number of consecutive identical calls before
// the evaluator forces ActionAsk, breaking potential infinite loops.
const doomLoopThreshold = 3
