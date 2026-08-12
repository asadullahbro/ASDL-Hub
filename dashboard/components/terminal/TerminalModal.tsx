'use client';

import { NodeTerminal } from './NodeTerminal';

interface TerminalModalProps {
  nodeId: string | null;
  onClose: () => void;
}

export function TerminalModal({ nodeId, onClose }: TerminalModalProps) {
  if (!nodeId) return null;
  return <NodeTerminal nodeId={nodeId} onClose={onClose} />;
}