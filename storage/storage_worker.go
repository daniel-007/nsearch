package storage

import (
	"sync"
	"nsearch/constant"
	"nsearch/indexer"
	"nsearch/utils"
	"bytes"
	"encoding/gob"
	"strings"
)

const (
	BOLT_FILE   = `./nsearch.bolt`
	SQLITE_FILE = `./nsearch.db`
)

//存储器工作协程
var (
	storage  *StorageWorker
	once     sync.Once
)

var engineMap = map[string]func(dbfile string) (Storage, error) {
	"bolt"    : NewBolt,
	"sqlite3" : NewSqlite,
}

type Storage interface {
	AddData(k, v []byte) error
	GetData(k []byte) (v []byte, err error)
	DelData(k []byte) error
	Close() error
}

type StorageWorker struct {
	inited      bool
	engine      string
	Istorage    Storage
	Record      chan map[string][]byte
	Srequest    chan *StorageRequest
}

type StorageRequest struct {
	DocId      int
	DocType    int

	Content    string
	WordsNum   float32
	Words      []string

	Delete     bool  //是否删除索引
}

func NewStorageWorker(engine string) *StorageWorker {
	if storage == nil || storage.inited != true {
		once.Do(func() {
			var istorage Storage
			var err error
			if function, has := engineMap[engine]; has {
				if engine == "bolt" {
					istorage, err = function(BOLT_FILE)
				} else if engine == "sqlite3" {
					istorage, err = function(SQLITE_FILE)
				}
				if err != nil {
					return
				}
			} else {
				istorage, err = function(BOLT_FILE)
				if err != nil {
					return
				}
			}

			storage = &StorageWorker {
				inited   : true,
				engine   : engine,
				Istorage : istorage,
				Record   : make(chan map[string][]byte, constant.CHAN_SIZE),
				Srequest : make(chan *StorageRequest, constant.CHAN_SIZE),
			}
		})
	}

	return storage
}

func (sw *StorageWorker) DoStorage() {
	go sw.AddRecord()
	go sw.DelRecord()
}

func (sw *StorageWorker) AddRecord() {
	for true {
		justdoit := <- sw.Record

		if len(justdoit) > 0 {
			for k, v := range justdoit {
				record, err := sw.Istorage.GetData([]byte(k))
				if len(record) > 0 && err == nil {
					var documents1 []*indexer.Document
					buf := bytes.NewReader(v)
					dec := gob.NewDecoder(buf)
					err = dec.Decode(&documents1)
					if err == nil {
						var documents2 []*indexer.Document
						buf2 := bytes.NewReader(record)
						dec2 := gob.NewDecoder(buf2)
						err = dec2.Decode(&documents2)
						if err == nil {
							//docs := append(documents1, documents2...) //merge documents, 纯合并所有记录

							var docs []*indexer.Document
							for _, doc1 := range documents1 {
								if indexer.GetDocByTypeId(documents2, doc1.DocType, doc1.DocId) == nil {
									//添加新doc记录
									docs = append(docs, doc1)
								} else {
									//对存在的doc记录内容进行更新
									indexer.UpdateDocByTypeId(documents2, doc1)
								}
							}

							for _, doc2 := range documents2 {
								docs = append(docs, doc2)
							}

							//gob encode
							var value bytes.Buffer
							enc := gob.NewEncoder(&value)
							err = enc.Encode(docs)
							if err == nil {
								sw.Istorage.AddData([]byte(k), value.Bytes())
							}
						}
					}
				} else {
					sw.Istorage.AddData([]byte(k), v)
				}
			}
		}
	}
}

//删除索引记录数据
func (sw *StorageWorker) DelRecord() {
	for true {
		request := <- sw.Srequest

		if request.DocId != 0 && request.DocType != 0 && request.Delete {
			for _, word := range request.Words {
				wsi := utils.GetWordsInfo(word)
				if wsi != nil {
					index_word := strings.TrimSpace(wsi[0])
					record, err := sw.Istorage.GetData([]byte(index_word))
					if len(record) > 0 && err == nil {
						var documents []*indexer.Document
						buf := bytes.NewReader(record)
						dec := gob.NewDecoder(buf)
						err = dec.Decode(&documents)
						if err == nil {
							var docs []*indexer.Document
							for i, doc := range documents {
								if doc.DocId == request.DocId && doc.DocType == request.DocType {
									if len(documents) > 1 {
										docs = append(documents[:i], documents[i+1:]...)
									} else if len(documents) == 1 {
										sw.Istorage.DelData([]byte(index_word))
									}
								}
							}

							//gob encode
							if len(docs) > 0 {
								var value bytes.Buffer
								enc := gob.NewEncoder(&value)
								err = enc.Encode(docs)
								if err == nil {
									sw.Istorage.AddData([]byte(index_word), value.Bytes())
								}
							}
						}
					}
				}
			}
		}
	}
}