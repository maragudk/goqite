create table queue (
  id text primary key default ('m_' || lower(hex(randomblob(16)))),
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  body blob not null,
  timeout text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  received integer not null default 0
) strict;

create trigger queue_updated_timestamp after update on queue begin
  update queue set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where id = old.id;
end;
