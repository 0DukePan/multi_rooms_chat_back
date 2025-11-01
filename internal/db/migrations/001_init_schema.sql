-- Create UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- Users table
CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  username TEXT UNIQUE NOT NULL,
  email TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  avatar_url TEXT,
  status TEXT DEFAULT 'offline' CHECK (status IN ('online', 'offline', 'away')),
  last_seen TIMESTAMPTZ DEFAULT NOW(),
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);

-- Rooms table
CREATE TABLE rooms (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('public', 'private', 'group', 'dm')),
  creator_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  topic TEXT,
  is_archived BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_rooms_type ON rooms(type);
CREATE INDEX idx_rooms_creator ON rooms(creator_id);
CREATE INDEX idx_rooms_archived ON rooms(is_archived);

-- Room members table
CREATE TABLE room_members (
  room_id UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT DEFAULT 'member' CHECK (role IN ('admin', 'member', 'moderator')),
  joined_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (room_id, user_id)
);

CREATE INDEX idx_room_members_user ON room_members(user_id);

-- Messages table
CREATE TABLE messages (
  id BIGSERIAL PRIMARY KEY,
  room_id UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  message_type TEXT DEFAULT 'text' CHECK (message_type IN ('text', 'image', 'file')),
  file_url TEXT,
  parent_id BIGINT REFERENCES messages(id) ON DELETE CASCADE,
  edited_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_messages_room ON messages(room_id, created_at DESC);
CREATE INDEX idx_messages_user ON messages(user_id);
CREATE INDEX idx_messages_created ON messages(created_at DESC);

-- Full-text search on messages
ALTER TABLE messages ADD COLUMN tsv tsvector GENERATED ALWAYS AS (
  to_tsvector('english', content)
) STORED;
CREATE INDEX idx_messages_fts ON messages USING GIN(tsv);

-- Message reads table
CREATE TABLE message_reads (
  message_id BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  read_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (message_id, user_id)
);

CREATE INDEX idx_message_reads_message ON message_reads(message_id);
CREATE INDEX idx_message_reads_user ON message_reads(user_id);

-- Reactions table
CREATE TABLE reactions (
  message_id BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  emoji TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY (message_id, user_id, emoji)
);

CREATE INDEX idx_reactions_message ON reactions(message_id);

-- Row Level Security (RLS) - Enable it
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE rooms ENABLE ROW LEVEL SECURITY;
ALTER TABLE room_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE message_reads ENABLE ROW LEVEL SECURITY;

-- RLS Policies for users (users can only read non-sensitive user data)
CREATE POLICY users_select ON users FOR SELECT
  USING (true);

-- RLS Policies for rooms (users can only see rooms they're members of)
CREATE POLICY rooms_select ON rooms FOR SELECT
  USING (EXISTS (
    SELECT 1 FROM room_members 
    WHERE room_members.room_id = rooms.id 
    AND room_members.user_id = current_user_id()
  ));

-- RLS Policies for messages (users can only see messages in rooms they're members of)
CREATE POLICY messages_select ON messages FOR SELECT
  USING (EXISTS (
    SELECT 1 FROM room_members 
    WHERE room_members.room_id = messages.room_id 
    AND room_members.user_id = current_user_id()
  ));

CREATE POLICY messages_insert ON messages FOR INSERT
  WITH CHECK (EXISTS (
    SELECT 1 FROM room_members 
    WHERE room_members.room_id = messages.room_id 
    AND room_members.user_id = current_user_id()
  ));

-- Helper function to get current user ID from JWT
CREATE OR REPLACE FUNCTION current_user_id() RETURNS UUID AS $$
  SELECT (current_setting('app.user_id', true))::UUID;
$$ LANGUAGE SQL;
