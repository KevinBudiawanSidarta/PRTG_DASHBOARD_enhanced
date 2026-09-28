'use client';
import { useCallback, useEffect, useRef, useState } from 'react';

const API = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
const ORG = process.env.NEXT_PUBLIC_ORGANIZATION_ID || '11111111-1111-1111-1111-111111111111';

type ChatRole = 'user' | 'assistant';
type ChatMessage = { role: ChatRole; content: string; error?: boolean };

const STARTERS = [
  'Ringkas kondisi bisnis saat ini',
  'Insiden mana yang paling kritis sekarang?',
  'Berapa total eksposur finansial bulan ini?',
  'Service apa yang paling berisiko?',
];

async function postChat(message: string, history: { role: ChatRole; content: string }[]) {
  const res = await fetch(`${API}/api/v1/ai/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Organization-ID': ORG },
    body: JSON.stringify({ message, history }),
    cache: 'no-store',
  });
  if (!res.ok) {
    const text = await res.text();
    let message = text;
    try {
      const parsed = JSON.parse(text);
      if (parsed?.error) message = parsed.error;
    } catch { /* not JSON, use raw text */ }
    throw new Error(message);
  }
  return res.json() as Promise<{ reply: string }>;
}

export default function AIChatbot() {
  const [open, setOpen] = useState(false);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' });
  }, [messages, loading]);

  useEffect(() => {
    if (open) textareaRef.current?.focus();
  }, [open]);

  const send = useCallback(async (text: string) => {
    const trimmed = text.trim();
    if (!trimmed || loading) return;
    const history = messages
      .filter(m => !m.error)
      .map(m => ({ role: m.role, content: m.content }));
    setMessages(m => [...m, { role: 'user', content: trimmed }]);
    setInput('');
    setLoading(true);
    try {
      const data = await postChat(trimmed, history);
      setMessages(m => [...m, { role: 'assistant', content: data.reply }]);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Gagal menghubungi AI agent';
      setMessages(m => [...m, { role: 'assistant', content: `⚠️ ${msg}`, error: true }]);
    } finally {
      setLoading(false);
    }
  }, [messages, loading]);

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      send(input);
    }
  }

  return (
    <>
      {/* Floating launcher button */}
      {!open && (
        <button
          onClick={() => setOpen(true)}
          id="ai-chatbot-launcher"
          aria-label="Buka AI Business Analyst"
          style={{
            position: 'fixed', right: 24, bottom: 24, zIndex: 1000,
            width: 60, height: 60, borderRadius: '50%', border: 'none', cursor: 'pointer',
            background: 'linear-gradient(135deg, var(--accent-from), var(--accent-to))',
            boxShadow: '0 8px 24px -4px var(--accent-glow), var(--shadow-lg)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 26, color: '#fff', transition: 'transform .15s ease',
          }}
          onMouseEnter={e => (e.currentTarget.style.transform = 'scale(1.08)')}
          onMouseLeave={e => (e.currentTarget.style.transform = 'scale(1)')}
        >
          <span style={{ animation: 'ai-chat-pulse 2.4s ease-in-out infinite' }}>✨</span>
        </button>
      )}

      {/* Chat panel */}
      {open && (
        <div
          id="ai-chatbot-panel"
          style={{
            position: 'fixed', right: 24, bottom: 24, zIndex: 1000,
            width: 'min(400px, calc(100vw - 32px))', height: 'min(600px, calc(100vh - 48px))',
            background: 'var(--bg-surface)', borderRadius: 'var(--radius)',
            boxShadow: 'var(--shadow-lg)', border: '1px solid var(--border)',
            display: 'flex', flexDirection: 'column', overflow: 'hidden',
            animation: 'ai-chat-in .2s ease',
          }}
        >
          {/* Header */}
          <div style={{
            padding: '16px 18px', background: 'linear-gradient(135deg, var(--accent-from), var(--accent-to))',
            display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexShrink: 0,
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <div style={{
                width: 34, height: 34, borderRadius: '50%', background: 'rgba(255,255,255,.2)',
                display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 17,
              }}>✨</div>
              <div>
                <div style={{ color: '#fff', fontWeight: 700, fontSize: 14, lineHeight: 1.2 }}>AI Business Analyst</div>
                <div style={{ color: 'rgba(255,255,255,.8)', fontSize: 11, display: 'flex', alignItems: 'center', gap: 5 }}>
                  <span style={{ width: 6, height: 6, borderRadius: '50%', background: '#4ade80', display: 'inline-block' }} />
                  Business Impact Analysis
                </div>
              </div>
            </div>
            <button
              onClick={() => setOpen(false)}
              aria-label="Tutup chat"
              style={{ background: 'rgba(255,255,255,.15)', border: 'none', borderRadius: 8, width: 28, height: 28, color: '#fff', cursor: 'pointer', fontSize: 15, lineHeight: 1 }}
            >✕</button>
          </div>

          {/* Messages */}
          <div ref={scrollRef} style={{ flex: 1, overflowY: 'auto', padding: '16px', display: 'flex', flexDirection: 'column', gap: 12 }}>
            {messages.length === 0 && (
              <div>
                <div style={{
                  background: 'var(--bg-raised)', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)',
                  padding: '12px 14px', fontSize: 13, color: 'var(--text-secondary)', marginBottom: 14,
                }}>
                  👋 Halo! Saya bisa bantu jawab pertanyaan seputar kondisi bisnis, insiden aktif, dan dampak finansial di dashboard ini — berdasarkan data terkini.
                </div>
                <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: .5, marginBottom: 8 }}>
                  Coba tanyakan:
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                  {STARTERS.map(s => (
                    <button
                      key={s}
                      onClick={() => send(s)}
                      style={{
                        textAlign: 'left', padding: '9px 12px', borderRadius: 'var(--radius-sm)',
                        border: '1px solid var(--border)', background: 'var(--bg-card)', color: 'var(--text-primary)',
                        fontSize: 12.5, cursor: 'pointer', transition: 'background .12s',
                      }}
                      onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-raised)')}
                      onMouseLeave={e => (e.currentTarget.style.background = 'var(--bg-card)')}
                    >
                      💬 {s}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {messages.map((m, i) => (
              <div key={i} style={{ display: 'flex', justifyContent: m.role === 'user' ? 'flex-end' : 'flex-start' }}>
                <div style={{
                  maxWidth: '85%', padding: '10px 13px', borderRadius: 'var(--radius-sm)', fontSize: 13, lineHeight: 1.5,
                  whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                  background: m.role === 'user'
                    ? 'linear-gradient(135deg, var(--accent-from), var(--accent-to))'
                    : m.error ? 'var(--red-bg)' : 'var(--bg-raised)',
                  color: m.role === 'user' ? '#fff' : m.error ? 'var(--red-text)' : 'var(--text-primary)',
                  border: m.role === 'assistant' && !m.error ? '1px solid var(--border)' : 'none',
                }}>
                  {m.content}
                </div>
              </div>
            ))}

            {loading && (
              <div style={{ display: 'flex', justifyContent: 'flex-start' }}>
                <div style={{
                  padding: '12px 16px', borderRadius: 'var(--radius-sm)', background: 'var(--bg-raised)',
                  border: '1px solid var(--border)', display: 'flex', gap: 4, alignItems: 'center',
                }}>
                  {[0, 1, 2].map(i => (
                    <span key={i} style={{
                      width: 6, height: 6, borderRadius: '50%', background: 'var(--text-muted)',
                      animation: `ai-chat-dot 1.2s ${i * 0.15}s ease-in-out infinite`,
                    }} />
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* Input */}
          <div style={{ padding: 12, borderTop: '1px solid var(--border)', display: 'flex', gap: 8, alignItems: 'flex-end', flexShrink: 0 }}>
            <textarea
              ref={textareaRef}
              value={input}
              onChange={e => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Tanyakan sesuatu tentang dashboard ini..."
              rows={1}
              style={{
                flex: 1, resize: 'none', maxHeight: 90, padding: '9px 12px', borderRadius: 'var(--radius-sm)',
                border: '1px solid var(--border)', fontSize: 13, fontFamily: 'inherit', color: 'var(--text-primary)',
                background: 'var(--bg-card)', outline: 'none',
              }}
            />
            <button
              onClick={() => send(input)}
              disabled={loading || !input.trim()}
              aria-label="Kirim pesan"
              style={{
                width: 38, height: 38, borderRadius: 'var(--radius-sm)', border: 'none', flexShrink: 0,
                background: loading || !input.trim() ? 'var(--bg-raised)' : 'linear-gradient(135deg, var(--accent-from), var(--accent-to))',
                color: loading || !input.trim() ? 'var(--text-muted)' : '#fff',
                cursor: loading || !input.trim() ? 'not-allowed' : 'pointer',
                fontSize: 15, display: 'flex', alignItems: 'center', justifyContent: 'center',
              }}
            >➤</button>
          </div>
        </div>
      )}

      <style>{`
        @keyframes ai-chat-in { from { opacity: 0; transform: translateY(12px) scale(.98); } to { opacity: 1; transform: translateY(0) scale(1); } }
        @keyframes ai-chat-pulse { 0%, 100% { transform: scale(1); } 50% { transform: scale(1.12); } }
        @keyframes ai-chat-dot { 0%, 60%, 100% { opacity: .3; transform: translateY(0); } 30% { opacity: 1; transform: translateY(-3px); } }
      `}</style>
    </>
  );
}
