'use client';

import { NodeTerminal } from './NodeTerminal';

interface TerminalModalProps {
  nodeId: string | null;
  nodeName?: string;
  onClose: () => void;
}

export function TerminalModal({ nodeId, nodeName, onClose }: TerminalModalProps) {
  if (!nodeId) return null;
  return <NodeTerminal nodeId={nodeId} nodeName={nodeName} onClose={onClose} />;
}
