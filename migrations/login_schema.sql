-- login_schema.sql — `nel` login/auth database (MySQL), Phase 4 Task 4.3.
--
-- Flattened from ryzomcore/web/private_php/setup/sql/nel_00001.sql with the
-- nel_00002..4 deltas applied (permission: PermissionId/DomainId replace
-- prim/ClientApplication; shard: PK on ShardId, patch-URL columns dropped;
-- user: Password VARCHAR(106)). Modernized for MySQL 8:
--   * zero-date defaults ('0000-00-00') -> nullable DATE/DATETIME, NULL
--     default (rejected by MySQL 8 strict mode; columns are billing-era
--     fields the login service never writes)
--   * MyISAM -> InnoDB
--   * post-hoc ALTER PRIMARY KEY statements folded into the CREATEs,
--     so the file is idempotent under IF NOT EXISTS
--
-- Mounted at /docker-entrypoint-initdb.d/ by docker-compose.dev.yml so a
-- fresh mysql service boots with the schema in place.
--
-- ADR-005: MySQL is authoritative for login/auth ONLY. Game data lives in
-- PostgreSQL (001_sheet_schema.sql). Do not add game tables here.

CREATE TABLE IF NOT EXISTS `domain` (
  `domain_id` int unsigned NOT NULL AUTO_INCREMENT,
  `domain_name` varchar(32) NOT NULL DEFAULT '',
  `status` enum('ds_close','ds_dev','ds_restricted','ds_open') NOT NULL DEFAULT 'ds_dev',
  `patch_version` int unsigned NOT NULL DEFAULT '0',
  `backup_patch_url` varchar(255) DEFAULT NULL,
  `patch_urls` text,
  `login_address` varchar(255) NOT NULL DEFAULT '',
  `session_manager_address` varchar(255) NOT NULL DEFAULT '',
  `ring_db_name` varchar(255) NOT NULL DEFAULT '',
  `web_host` varchar(255) NOT NULL DEFAULT '',
  `web_host_php` varchar(255) NOT NULL DEFAULT '',
  `description` varchar(200) DEFAULT NULL,
  PRIMARY KEY (`domain_id`),
  UNIQUE KEY `name_idx` (`domain_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `permission` (
  `PermissionId` int NOT NULL AUTO_INCREMENT,
  `UId` int unsigned NOT NULL DEFAULT '0',
  `DomainId` int NOT NULL DEFAULT '-1',
  `ShardId` int NOT NULL DEFAULT '-1',
  `AccessPrivilege` set('OPEN','DEV','RESTRICTED') NOT NULL DEFAULT 'OPEN',
  PRIMARY KEY (`PermissionId`),
  KEY `UIDIndex` (`UId`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `shard` (
  `ShardId` int NOT NULL DEFAULT '0',
  `domain_id` int unsigned NOT NULL DEFAULT '0',
  `WsAddr` varchar(64) DEFAULT NULL,
  `NbPlayers` int unsigned DEFAULT '0',
  `Name` varchar(255) DEFAULT 'unknown shard',
  `Online` tinyint unsigned DEFAULT '0',
  `Version` varchar(64) NOT NULL DEFAULT '',
  `FixedSessionId` int unsigned NOT NULL DEFAULT '0',
  `State` enum('ds_close','ds_dev','ds_restricted','ds_open') NOT NULL DEFAULT 'ds_dev',
  `MOTD` text NOT NULL,
  PRIMARY KEY (`ShardId`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='contains all shards information for login system';

CREATE TABLE IF NOT EXISTS `user` (
  `UId` int NOT NULL AUTO_INCREMENT,
  `Login` varchar(64) NOT NULL DEFAULT '',
  `Password` varchar(106) DEFAULT NULL,
  `ShardId` int NOT NULL DEFAULT '-1',
  `State` enum('Offline','Online') NOT NULL DEFAULT 'Offline',
  `Privilege` varchar(255) NOT NULL DEFAULT '',
  `GroupName` varchar(255) NOT NULL DEFAULT '',
  `FirstName` varchar(255) NOT NULL DEFAULT '',
  `LastName` varchar(255) NOT NULL DEFAULT '',
  `Birthday` varchar(32) NOT NULL DEFAULT '',
  `Gender` tinyint unsigned NOT NULL DEFAULT '0',
  `Country` char(2) NOT NULL DEFAULT '',
  `Email` varchar(255) NOT NULL DEFAULT '',
  `Address` varchar(255) NOT NULL DEFAULT '',
  `City` varchar(100) NOT NULL DEFAULT '',
  `PostalCode` varchar(10) NOT NULL DEFAULT '',
  `USState` char(2) NOT NULL DEFAULT '',
  `Chat` char(2) NOT NULL DEFAULT '0',
  `BetaKeyId` int unsigned NOT NULL DEFAULT '0',
  `CachedCoupons` varchar(255) NOT NULL DEFAULT '',
  `ProfileAccess` varchar(45) DEFAULT NULL,
  `Level` int NOT NULL DEFAULT '0',
  `CurrentFunds` int NOT NULL DEFAULT '0',
  `IdBilling` varchar(255) NOT NULL DEFAULT '',
  `Community` char(2) NOT NULL DEFAULT '--',
  `Newsletter` tinyint(1) NOT NULL DEFAULT '1',
  `Account` varchar(64) NOT NULL DEFAULT '',
  `ChoiceSubLength` tinyint NOT NULL DEFAULT '0',
  `CurrentSubLength` varchar(255) NOT NULL DEFAULT '0',
  `ValidIdBilling` int NOT NULL DEFAULT '0',
  `GMId` int NOT NULL DEFAULT '0',
  `ExtendedPrivilege` varchar(128) NOT NULL DEFAULT '',
  `ToolsGroup` varchar(20) NOT NULL DEFAULT '',
  `Unsubscribe` date DEFAULT NULL,
  `SubDate` datetime DEFAULT NULL,
  `SubIp` varchar(20) NOT NULL DEFAULT '',
  `SecurePassword` varchar(32) NOT NULL DEFAULT '',
  `LastInvoiceEmailCheck` date DEFAULT NULL,
  `FromSource` varchar(8) NOT NULL DEFAULT '',
  `ValidMerchantCode` varchar(13) NOT NULL DEFAULT '',
  `PBC` tinyint(1) NOT NULL DEFAULT '0',
  `ApiKeySeed` varchar(8) DEFAULT NULL,
  PRIMARY KEY (`UId`),
  UNIQUE KEY `LoginIndex` (`Login`),
  UNIQUE KEY `EmailIndex` (`Email`),
  KEY `GroupIndex` (`GroupName`),
  KEY `ToolsGroup` (`ToolsGroup`),
  KEY `CurrentSubLength` (`CurrentSubLength`),
  KEY `Community` (`Community`),
  KEY `GMId` (`GMId`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='contains all users information for login system';
