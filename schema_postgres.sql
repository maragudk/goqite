create extension if not exists pgcrypto;

create function update_timestamp()
returns trigger as $$
begin
   new.updated = now();
   return new;
end;
$$ language plpgsql;

create table goqite (
  id text primary key default ('m_' || encode(gen_random_bytes(16), 'hex')),
  created timestamptz not null default now(),
  updated timestamptz not null default now(),
  queue text not null,
  body bytea not null,
  timeout timestamptz not null default now(),
  received integer not null default 0,
  priority integer not null default 0
);

create trigger goqite_updated_timestamp
before update on goqite
for each row execute procedure update_timestamp();

create index goqite_queue_priority_created_idx on goqite (queue, priority desc, created);
