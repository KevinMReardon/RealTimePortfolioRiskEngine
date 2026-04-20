CREATE TABLE IF NOT EXISTS agent_session_tool_calls (
    id BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL,
    seq_no INT NOT NULL,
    tool_name TEXT NOT NULL,
    tool_input JSONB NOT NULL,
    tool_output JSONB NULL,
    latency_ms INT NULL,
    success BOOLEAN NOT NULL DEFAULT false,
    error_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_session_tool_calls_session
        FOREIGN KEY (session_id) REFERENCES agent_sessions(session_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_session_tool_calls_seq
    ON agent_session_tool_calls (session_id, seq_no);
