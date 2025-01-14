// Copyright 2022 Liuxiangchao iwind.liu@gmail.com. All rights reserved.

package caches

import (
	"database/sql"
	"errors"
	teaconst "github.com/TeaOSLab/EdgeNode/internal/const"
	"github.com/TeaOSLab/EdgeNode/internal/utils"
	"github.com/TeaOSLab/EdgeNode/internal/utils/dbs"
	"github.com/iwind/TeaGo/types"
	timeutil "github.com/iwind/TeaGo/utils/time"
	"runtime"
	"strings"
	"time"
)

type FileListDB struct {
	readDB  *dbs.DB
	writeDB *dbs.DB

	itemsTableName string
	hitsTableName  string

	total int64

	isClosed bool
	isReady  bool

	// cacheItems
	existsByHashStmt   *dbs.Stmt // 根据hash检查是否存在
	insertStmt         *dbs.Stmt // 写入数据
	selectByHashStmt   *dbs.Stmt // 使用hash查询数据
	deleteByHashStmt   *dbs.Stmt // 根据hash删除数据
	statStmt           *dbs.Stmt // 统计
	purgeStmt          *dbs.Stmt // 清理
	deleteAllStmt      *dbs.Stmt // 删除所有数据
	listOlderItemsStmt *dbs.Stmt // 读取较早存储的缓存

	// hits
	insertHitStmt       *dbs.Stmt // 写入数据
	increaseHitStmt     *dbs.Stmt // 增加点击量
	deleteHitByHashStmt *dbs.Stmt // 根据hash删除数据
	lfuHitsStmt         *dbs.Stmt // 读取老的数据
}

func NewFileListDB() *FileListDB {
	return &FileListDB{}
}

func (this *FileListDB) Open(dbPath string) error {
	// write db
	writeDB, err := sql.Open("sqlite3", "file:"+dbPath+"?cache=private&mode=rwc&_journal_mode=WAL&_sync=OFF&_cache_size=32000&_secure_delete=FAST")
	if err != nil {
		return errors.New("open write database failed: " + err.Error())
	}

	writeDB.SetMaxOpenConns(1)

	// TODO 耗时过长，暂时不整理数据库
	// TODO 需要根据行数来判断是否VACUUM
	/**_, err = db.Exec("VACUUM")
	if err != nil {
		return err
	}**/

	this.writeDB = dbs.NewDB(writeDB)

	if teaconst.EnableDBStat {
		this.writeDB.EnableStat(true)
	}

	// read db
	readDB, err := sql.Open("sqlite3", "file:"+dbPath+"?cache=private&mode=ro&_journal_mode=WAL&_sync=OFF&_cache_size=32000")
	if err != nil {
		return errors.New("open read database failed: " + err.Error())
	}

	readDB.SetMaxOpenConns(runtime.NumCPU())

	this.readDB = dbs.NewDB(readDB)

	if teaconst.EnableDBStat {
		this.readDB.EnableStat(true)
	}

	return nil
}

func (this *FileListDB) Init() error {
	this.itemsTableName = "cacheItems"
	this.hitsTableName = "hits"

	// 创建
	var err = this.initTables(1)
	if err != nil {
		return errors.New("init tables failed: " + err.Error())
	}

	// 读取总数量
	row := this.readDB.QueryRow(`SELECT COUNT(*) FROM "` + this.itemsTableName + `"`)
	if row.Err() != nil {
		return row.Err()
	}
	var total int64
	err = row.Scan(&total)
	if err != nil {
		return err
	}
	this.total = total

	// 常用语句
	this.existsByHashStmt, err = this.readDB.Prepare(`SELECT "expiredAt" FROM "` + this.itemsTableName + `" INDEXED BY "hash" WHERE "hash"=? AND expiredAt>? LIMIT 1`)
	if err != nil {
		return err
	}

	this.insertStmt, err = this.writeDB.Prepare(`INSERT INTO "` + this.itemsTableName + `" ("hash", "key", "headerSize", "bodySize", "metaSize", "expiredAt", "staleAt", "host", "serverId", "createdAt") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}

	this.selectByHashStmt, err = this.readDB.Prepare(`SELECT "key", "headerSize", "bodySize", "metaSize", "expiredAt" FROM "` + this.itemsTableName + `" WHERE "hash"=? LIMIT 1`)
	if err != nil {
		return err
	}

	this.deleteByHashStmt, err = this.writeDB.Prepare(`DELETE FROM "` + this.itemsTableName + `" WHERE "hash"=?`)
	if err != nil {
		return err
	}

	this.statStmt, err = this.readDB.Prepare(`SELECT COUNT(*), IFNULL(SUM(headerSize+bodySize+metaSize), 0), IFNULL(SUM(headerSize+bodySize), 0) FROM "` + this.itemsTableName + `"`)
	if err != nil {
		return err
	}

	this.purgeStmt, err = this.readDB.Prepare(`SELECT "hash" FROM "` + this.itemsTableName + `" WHERE staleAt<=? LIMIT ?`)
	if err != nil {
		return err
	}

	this.deleteAllStmt, err = this.writeDB.Prepare(`DELETE FROM "` + this.itemsTableName + `"`)
	if err != nil {
		return err
	}

	this.listOlderItemsStmt, err = this.readDB.Prepare(`SELECT "hash" FROM "` + this.itemsTableName + `" ORDER BY "id" ASC LIMIT ?`)

	this.insertHitStmt, err = this.writeDB.Prepare(`INSERT INTO "` + this.hitsTableName + `" ("hash", "week2Hits", "week") VALUES (?, 1, ?)`)

	this.increaseHitStmt, err = this.writeDB.Prepare(`INSERT INTO "` + this.hitsTableName + `" ("hash", "week2Hits", "week") VALUES (?, 1, ?) ON CONFLICT("hash") DO UPDATE SET "week1Hits"=IIF("week"=?, "week1Hits", "week2Hits"), "week2Hits"=IIF("week"=?, "week2Hits"+1, 1), "week"=?`)
	if err != nil {
		return err
	}

	this.deleteHitByHashStmt, err = this.writeDB.Prepare(`DELETE FROM "` + this.hitsTableName + `" WHERE "hash"=?`)
	if err != nil {
		return err
	}

	this.lfuHitsStmt, err = this.readDB.Prepare(`SELECT "hash" FROM "` + this.hitsTableName + `" ORDER BY "week" ASC, "week1Hits"+"week2Hits" ASC LIMIT ?`)
	if err != nil {
		return err
	}

	this.isReady = true

	return nil
}

func (this *FileListDB) IsReady() bool {
	return this.isReady
}

func (this *FileListDB) Total() int64 {
	return this.total
}

func (this *FileListDB) Add(hash string, item *Item) error {
	if item.StaleAt == 0 {
		item.StaleAt = item.ExpiredAt
	}

	// 放入队列
	_, err := this.insertStmt.Exec(hash, item.Key, item.HeaderSize, item.BodySize, item.MetaSize, item.ExpiredAt, item.StaleAt, item.Host, item.ServerId, utils.UnixTime())
	if err != nil {
		return err
	}

	return nil
}

func (this *FileListDB) ListExpiredItems(count int) (hashList []string, err error) {
	if !this.isReady {
		return nil, nil
	}

	if count <= 0 {
		count = 100
	}

	rows, err := this.purgeStmt.Query(time.Now().Unix(), count)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var hash string
		err = rows.Scan(&hash)
		if err != nil {
			return nil, err
		}
		hashList = append(hashList, hash)
	}
	return hashList, nil
}

func (this *FileListDB) ListLFUItems(count int) (hashList []string, err error) {
	if !this.isReady {
		return nil, nil
	}

	if count <= 0 {
		count = 100
	}

	hashList, err = this.listLFUItems(count)
	if err != nil {
		return
	}

	if len(hashList) > count/2 {
		return
	}

	// 不足补齐
	olderHashList, err := this.listOlderItems(count - len(hashList))
	if err != nil {
		return nil, err
	}
	hashList = append(hashList, olderHashList...)
	return
}

func (this *FileListDB) IncreaseHit(hash string) error {
	var week = timeutil.Format("YW")
	_, err := this.increaseHitStmt.Exec(hash, week, week, week, week)
	return err
}

func (this *FileListDB) CleanPrefix(prefix string) error {
	if !this.isReady {
		return nil
	}
	var count = int64(10000)
	var staleLife = 600             // TODO 需要可以设置
	var unixTime = utils.UnixTime() // 只删除当前的，不删除新的
	for {
		result, err := this.writeDB.Exec(`UPDATE "`+this.itemsTableName+`" SET expiredAt=0,staleAt=? WHERE id IN (SELECT id FROM "`+this.itemsTableName+`" WHERE expiredAt>0 AND createdAt<=? AND INSTR("key", ?)=1 LIMIT `+types.String(count)+`)`, unixTime+int64(staleLife), unixTime, prefix)
		if err != nil {
			return err
		}
		affectedRows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affectedRows < count {
			return nil
		}
	}
}

func (this *FileListDB) CleanAll() error {
	if !this.isReady {
		return nil
	}

	_, err := this.deleteAllStmt.Exec()
	if err != nil {
		return err
	}

	return nil
}

func (this *FileListDB) Close() error {
	this.isClosed = true
	this.isReady = false

	if this.existsByHashStmt != nil {
		_ = this.existsByHashStmt.Close()
	}
	if this.insertStmt != nil {
		_ = this.insertStmt.Close()
	}
	if this.selectByHashStmt != nil {
		_ = this.selectByHashStmt.Close()
	}
	if this.deleteByHashStmt != nil {
		_ = this.deleteByHashStmt.Close()
	}
	if this.statStmt != nil {
		_ = this.statStmt.Close()
	}
	if this.purgeStmt != nil {
		_ = this.purgeStmt.Close()
	}
	if this.deleteAllStmt != nil {
		_ = this.deleteAllStmt.Close()
	}
	if this.listOlderItemsStmt != nil {
		_ = this.listOlderItemsStmt.Close()
	}

	if this.insertHitStmt != nil {
		_ = this.insertHitStmt.Close()
	}
	if this.increaseHitStmt != nil {
		_ = this.increaseHitStmt.Close()
	}
	if this.deleteHitByHashStmt != nil {
		_ = this.deleteHitByHashStmt.Close()
	}
	if this.lfuHitsStmt != nil {
		_ = this.lfuHitsStmt.Close()
	}

	var errStrings []string

	if this.readDB != nil {
		err := this.readDB.Close()
		if err != nil {
			errStrings = append(errStrings, err.Error())
		}
	}

	if this.writeDB != nil {
		err := this.writeDB.Close()
		if err != nil {
			errStrings = append(errStrings, err.Error())
		}
	}

	if len(errStrings) == 0 {
		return nil
	}
	return errors.New("close database failed: " + strings.Join(errStrings, ", "))
}

// 初始化
func (this *FileListDB) initTables(times int) error {
	{
		// expiredAt - 过期时间，用来判断有无过期
		// staleAt - 过时缓存最大时间，用来清理缓存
		_, err := this.writeDB.Exec(`CREATE TABLE IF NOT EXISTS "` + this.itemsTableName + `" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "hash" varchar(32),
  "key" varchar(1024),
  "tag" varchar(64),
  "headerSize" integer DEFAULT 0,
  "bodySize" integer DEFAULT 0,
  "metaSize" integer DEFAULT 0,
  "expiredAt" integer DEFAULT 0,
  "staleAt" integer DEFAULT 0,
  "createdAt" integer DEFAULT 0,
  "host" varchar(128),
  "serverId" integer
);

DROP INDEX IF EXISTS "createdAt";
DROP INDEX IF EXISTS "expiredAt";
DROP INDEX IF EXISTS "serverId";

CREATE INDEX IF NOT EXISTS "staleAt"
ON "` + this.itemsTableName + `" (
  "staleAt" ASC
);

CREATE UNIQUE INDEX IF NOT EXISTS "hash"
ON "` + this.itemsTableName + `" (
  "hash" ASC
);
`)

		if err != nil {
			// 尝试删除重建
			if times < 3 {
				_, dropErr := this.writeDB.Exec(`DROP TABLE "` + this.itemsTableName + `"`)
				if dropErr == nil {
					return this.initTables(times + 1)
				}
				return err
			}

			return err
		}
	}

	{
		_, err := this.writeDB.Exec(`CREATE TABLE IF NOT EXISTS "` + this.hitsTableName + `" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "hash" varchar(32),
  "week1Hits" integer DEFAULT 0,
  "week2Hits" integer DEFAULT 0,
  "week" varchar(6)
);

CREATE UNIQUE INDEX IF NOT EXISTS "hits_hash"
ON "` + this.hitsTableName + `" (
  "hash" ASC
);
`)
		if err != nil {
			// 尝试删除重建
			if times < 3 {
				_, dropErr := this.writeDB.Exec(`DROP TABLE "` + this.hitsTableName + `"`)
				if dropErr == nil {
					return this.initTables(times + 1)
				}
				return err
			}

			return err
		}
	}

	return nil
}

func (this *FileListDB) listLFUItems(count int) (hashList []string, err error) {
	rows, err := this.lfuHitsStmt.Query(count)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var hash string
		err = rows.Scan(&hash)
		if err != nil {
			return nil, err
		}
		hashList = append(hashList, hash)
	}

	return hashList, nil
}

func (this *FileListDB) listOlderItems(count int) (hashList []string, err error) {
	rows, err := this.listOlderItemsStmt.Query(count)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var hash string
		err = rows.Scan(&hash)
		if err != nil {
			return nil, err
		}
		hashList = append(hashList, hash)
	}

	return hashList, nil
}
