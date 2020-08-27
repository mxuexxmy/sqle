package model

import (
	"fmt"

	"actiontech.cloud/universe/sqle/v3/sqle/errors"

	"github.com/jinzhu/gorm"
	"github.com/pingcap/parser/ast"
)

// task action
const (
	TASK_ACTION_INSPECT = iota + 1
	TASK_ACTION_COMMIT
	TASK_ACTION_ROLLBACK
)

const (
	TASK_ACTION_INIT                  = ""
	TASK_ACTION_DOING                 = "doing"
	TASK_ACTION_DONE                  = "finished"
	TASK_ACTION_ERROR                 = "failed"
	SQL_TYPE_DML                      = "dml"
	SQL_TYPE_DDL                      = "ddl"
	SQL_TYPE_MULTI                    = "dml&ddl"
	SQL_TYPE_PROCEDURE_FUNCTION       = "procedure&function"
	SQL_TYPE_PROCEDURE_FUNCTION_MULTI = "procedure&function&dml&ddl"
)

var ActionMap = map[int]string{
	TASK_ACTION_INSPECT:  "",
	TASK_ACTION_COMMIT:   "",
	TASK_ACTION_ROLLBACK: "",
}

type Sql struct {
	Model
	TaskId          uint       `json:"-"`
	Number          uint       `json:"number"`
	Content         string     `json:"sql" gorm:"type:text"`
	StartBinlogFile string     `json:"start_binlog_file"`
	StartBinlogPos  int64      `json:"start_binlog_pos"`
	EndBinlogFile   string     `json:"end_binlog_file"`
	EndBinlogPos    int64      `json:"end_binlog_pos"`
	RowAffects      int64      `json:"row_affects"`
	ExecStatus      string     `json:"exec_status"`
	ExecResult      string     `json:"exec_result"`
	Stmts           []ast.Node `json:"-" gorm:"-"`
}

type CommitSql struct {
	Sql
	InspectStatus string `json:"inspect_status"`
	InspectResult string `json:"inspect_result" gorm:"type:text"`
	// level: error, warn, notice, normal
	InspectLevel string `json:"inspect_level"`
}

func (s CommitSql) TableName() string {
	return "commit_sql_detail"
}

type RollbackSql struct {
	Sql
	CommitSqlNumber uint `json:"commit_sql_number"`
}

func (s RollbackSql) TableName() string {
	return "rollback_sql_detail"
}

type Task struct {
	Model
	Name         string         `json:"name" example:"REQ201812578"`
	Desc         string         `json:"desc" example:"this is a task"`
	Schema       string         `json:"schema" example:"db1"`
	Instance     *Instance      `json:"-" gorm:"foreignkey:InstanceId"`
	InstanceId   uint           `json:"instance_id"`
	InstanceName string         `json:"instance_name"`
	NormalRate   float64        `json:"normal_rate"`
	SqlType      string         `json:"-"`
	Action       uint           `json:"-"`
	ExecStatus   string         `json:"-"`
	CommitSqls   []*CommitSql   `json:"-" gorm:"foreignkey:TaskId"`
	RollbackSqls []*RollbackSql `json:"-" gorm:"foreignkey:TaskId"`
}

type TaskDetail struct {
	Task
	Instance     *Instance      `json:"instance"`
	InstanceId   uint           `json:"-"`
	CommitSqls   []*CommitSql   `json:"commit_sql_list"`
	RollbackSqls []*RollbackSql `json:"rollback_sql_list"`
}

func (t *Task) Detail() TaskDetail {
	data := TaskDetail{
		Task:         *t,
		InstanceId:   t.InstanceId,
		Instance:     t.Instance,
		CommitSqls:   t.CommitSqls,
		RollbackSqls: t.RollbackSqls,
	}
	if t.RollbackSqls == nil {
		data.RollbackSqls = []*RollbackSql{}
	}
	if t.CommitSqls == nil {
		data.CommitSqls = []*CommitSql{}
	}
	return data
}

func (t *Task) HasDoingAdvise() bool {
	if t.CommitSqls != nil {
		for _, commitSql := range t.CommitSqls {
			if commitSql.InspectStatus != TASK_ACTION_INIT {
				return true
			}
		}
	}
	return false
}

func (t *Task) HasDoingCommit() bool {
	if t.CommitSqls != nil {
		for _, commitSql := range t.CommitSqls {
			if commitSql.ExecStatus != TASK_ACTION_INIT {
				return true
			}
		}
	}
	return false
}

func (t *Task) IsCommitFailed() bool {
	if t.CommitSqls != nil {
		for _, commitSql := range t.CommitSqls {
			if commitSql.ExecStatus == TASK_ACTION_ERROR {
				return true
			}
		}
	}
	return false
}

func (t *Task) HasDoingRollback() bool {
	if t.RollbackSqls != nil {
		for _, rollbackSql := range t.RollbackSqls {
			if rollbackSql.ExecStatus != TASK_ACTION_INIT {
				return true
			}
		}
	}
	return false
}

func (t *Task) ValidAction(typ int) error {
	switch typ {
	case TASK_ACTION_INSPECT:
		// inspect sql allowed at all times
		return nil
	case TASK_ACTION_COMMIT:
		if t.HasDoingCommit() {
			return errors.New(errors.TASK_ACTION_DONE, fmt.Errorf("task has committed"))
		}
	case TASK_ACTION_ROLLBACK:
		if t.HasDoingRollback() {
			return errors.New(errors.TASK_ACTION_DONE, fmt.Errorf("task has rolled back"))
		}
		if t.IsCommitFailed() {
			return errors.New(errors.TASK_ACTION_INVALID, fmt.Errorf("task is commit failed, not allow rollback"))
		}
		if !t.HasDoingCommit() {
			return errors.New(errors.TASK_ACTION_INVALID, fmt.Errorf("task need commit first"))
		}
	}
	return nil
}

func (s *Storage) GetTaskById(taskId string) (*Task, bool, error) {
	task := &Task{}
	err := s.db.Preload("Instance").Preload("CommitSqls").Preload("RollbackSqls").First(&task, taskId).Error
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	return task, true, errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) GetTasks() ([]Task, error) {
	tasks := []Task{}
	err := s.db.Find(&tasks).Error
	return tasks, errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) GetTasksByIds(ids []string) ([]Task, error) {
	tasks := []Task{}
	err := s.db.Where("id IN (?)", ids).Find(&tasks).Error
	return tasks, errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) UpdateTask(task *Task, attrs ...interface{}) error {
	err := s.db.Table("tasks").Where("id = ?", task.ID).Update(attrs...).Error
	return errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) UpdateCommitSql(task *Task, commitSql []*CommitSql) error {
	err := s.db.Model(task).Association("CommitSqls").Replace(commitSql).Error
	return errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) UpdateRollbackSql(task *Task, rollbackSql []*RollbackSql) error {
	err := s.db.Model(task).Association("RollbackSqls").Replace(rollbackSql).Error
	return errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) UpdateCommitSqlById(commitSqlId string, attrs ...interface{}) error {
	err := s.db.Table(CommitSql{}.TableName()).Where("id = ?", commitSqlId).Update(attrs...).Error
	return errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) UpdateCommitSqlStatus(sql *Sql, status, result string) error {
	attr := map[string]interface{}{}
	if status != "" {
		sql.ExecStatus = status
		attr["exec_status"] = status
	}
	if result != "" {
		sql.ExecResult = result
		attr["exec_result"] = result
	}
	return s.UpdateCommitSqlById(fmt.Sprintf("%v", sql.ID), attr)
}

func (s *Storage) HardDeleteRollbackSqlByTaskIds(taskIds []string) error {
	rollbackSql := RollbackSql{}
	err := s.db.Unscoped().Where("task_id IN (?)", taskIds).Delete(rollbackSql).Error
	return errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) GetRollbackSqlByTaskId(taskId string, commitSqlNum []string) ([]RollbackSql, error) {
	rollbackSqls := []RollbackSql{}
	querySql := "task_id=?"
	queryArgs := make([]interface{}, 0)
	queryArgs = append(queryArgs, taskId)
	if len(commitSqlNum) > 0 {
		querySql += " AND COMMIT_SQL_NUMBER IN (?)"
		queryArgs = append(queryArgs, commitSqlNum)
	}
	err := s.db.Where(querySql, queryArgs...).Find(&rollbackSqls).Error
	return rollbackSqls, errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) UpdateRollbackSqlById(rollbackSqlId string, attrs ...interface{}) error {
	err := s.db.Table(RollbackSql{}.TableName()).Where("id = ?", rollbackSqlId).Update(attrs...).Error
	return errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) UpdateRollbackSqlStatus(sql *Sql, status, result string) error {
	attr := map[string]interface{}{}
	if status != "" {
		sql.ExecStatus = status
		attr["exec_status"] = status
	}
	if result != "" {
		sql.ExecResult = result
		attr["exec_result"] = result
	}
	return s.UpdateRollbackSqlById(fmt.Sprintf("%v", sql.ID), attr)
}

func (s *Storage) GetRelatedDDLTask(task *Task) ([]Task, error) {
	tasks := []Task{}
	err := s.db.Where(Task{
		InstanceId: task.InstanceId,
		Schema:     task.Schema,
		NormalRate: 1,
		SqlType:    SQL_TYPE_DDL,
		Action:     TASK_ACTION_INSPECT,
	}).Preload("Instance").Preload("CommitSqls").Find(&tasks).Error
	return tasks, errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) HardDeleteSqlCommittingResultByTaskIds(ids []string) error {
	CommitSql := CommitSql{}
	err := s.db.Unscoped().Where("task_id IN (?)", ids).Delete(CommitSql).Error
	return errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) GetExecErrorCommitSqlsByTaskId(taskId string) ([]CommitSql, error) {
	CommitSqls := []CommitSql{}
	err := s.db.Not("exec_result", []string{"ok", ""}).Where("task_id=? ", taskId).Find(&CommitSqls).Error
	return CommitSqls, errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) GetUploadedSqls(taskId, filterSqlExecutionStatus, filterSqlAuditStatus string, pageIndex, pageSize int) ([]CommitSql, uint32, error) {
	var count uint32
	CommitSqls := []CommitSql{}
	queryFilter := "task_id=?"
	queryArgs := make([]interface{}, 0)
	queryArgs = append(queryArgs, taskId)
	if filterSqlExecutionStatus != "" {
		if filterSqlExecutionStatus == "initialized" {
			filterSqlExecutionStatus = ""
		}
		queryFilter += " AND exec_status=?"
		queryArgs = append(queryArgs, filterSqlExecutionStatus)
	}
	if filterSqlAuditStatus != "" {
		queryFilter += " AND inspect_status=?"
		queryArgs = append(queryArgs, filterSqlAuditStatus)
	}
	if pageSize == 0 {
		err := s.db.Where(queryFilter, queryArgs...).Find(&CommitSqls).Count(&count).Error
		return CommitSqls, count, errors.New(errors.CONNECT_STORAGE_ERROR, err)
	}
	err := s.db.Model(&CommitSql{}).Where(queryFilter, queryArgs...).Count(&count).Error
	if err != nil {
		return CommitSqls, 0, errors.New(errors.CONNECT_STORAGE_ERROR, err)
	}

	err = s.db.Offset((pageIndex-1)*pageSize).Limit(pageSize).Where(queryFilter, queryArgs...).Find(&CommitSqls).Error

	return CommitSqls, count, errors.New(errors.CONNECT_STORAGE_ERROR, err)

}

func (s *Storage) DeleteTasksByIds(ids []string) error {
	tasks := Task{}
	err := s.db.Where("id IN (?)", ids).Delete(tasks).Error
	return errors.New(errors.CONNECT_STORAGE_ERROR, err)
}

func (s *Storage) HardDeleteTasksByIds(ids []string) error {
	tasks := Task{}
	err := s.db.Unscoped().Where("id IN (?)", ids).Delete(tasks).Error
	return errors.New(errors.CONNECT_STORAGE_ERROR, err)
}