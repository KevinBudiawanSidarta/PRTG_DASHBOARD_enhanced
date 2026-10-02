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
      setMessages(m => [...m, { role: 'assistant', content: `${msg}`, error: true }]);
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
            height: 40, padding: '0 18px', borderRadius: 99, border: 'none', cursor: 'pointer',
            background: 'var(--accent)', color: '#fff',
            fontSize: 13, fontWeight: 600, fontFamily: 'inherit',
          }}
        >
          Tanya AI
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
          }}
        >
          {/* Header */}
          <div style={{
            padding: '14px 16px', borderBottom: '1px solid var(--border)',
            display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexShrink: 0,
          }}>
            <div>
              <div style={{ color: 'var(--text-primary)', fontWeight: 600, fontSize: 14, lineHeight: 1.2 }}>AI Business Analyst</div>
              <div style={{ color: 'var(--text-muted)', fontSize: 11, marginTop: 2 }}>Business Impact Analysis</div>
            </div>
            <button
              onClick={() => setOpen(false)}
              aria-label="Tutup chat"
              style={{ background: 'transparent', border: 'none', borderRadius: 'var(--radius-xs)', width: 28, height: 28, color: 'var(--text-muted)', cursor: 'pointer', fontSize: 15, lineHeight: 1 }}
            >✕</button>
          </div>

          {/* Messages */}
          <div ref={scrollRef} style={{ flex: 1, overflowY: 'auto', padding: '16px', display: 'flex', flexDirection: 'column', gap: 12 }}>
            {messages.length === 0 && (
              <div>
                <div style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.5, marginBottom: 14 }}>
                  Halo! Saya bisa bantu jawab pertanyaan seputar kondisi bisnis, insiden aktif, dan dampak finansial di dashboard ini — berdasarkan data terkini.
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
                      {s}
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
                    ? 'var(--accent)'
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
                height: 38, padding: '0 14px', borderRadius: 'var(--radius-sm)', border: 'none', flexShrink: 0,
                background: loading || !input.trim() ? 'var(--bg-raised)' : 'var(--accent)',
                color: loading || !input.trim() ? 'var(--text-muted)' : '#fff',
                cursor: loading || !input.trim() ? 'not-allowed' : 'pointer',
                fontSize: 13, fontWeight: 600, fontFamily: 'inherit',
              }}
            >Kirim</button>
          </div>
        </div>
      )}

      <style>{`
        @keyframes ai-chat-dot { 0%, 60%, 100% { opacity: .3; transform: translateY(0); } 30% { opacity: 1; transform: translateY(-3px); } }
      `}</style>
    </>
  );
}
