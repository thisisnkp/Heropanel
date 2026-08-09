-- Port ranges for firewall rules (0 = single port, given by port).
ALTER TABLE firewall_rules ADD COLUMN port_end INTEGER NOT NULL DEFAULT 0;
