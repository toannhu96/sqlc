package catalog

import (
	"errors"

	"github.com/kyleconroy/sqlc/internal/sql/ast"
)

func Build(stmts []ast.Statement) (*Catalog, error) {
	c := &Catalog{
		DefaultSchema: "main", // TODO: Needs to be public for PostgreSQL
		Schemas: []*Schema{
			&Schema{Name: "main"},
		},
	}
	for i := range stmts {
		if stmts[i].Raw == nil {
			continue
		}
		var err error
		switch n := stmts[i].Raw.Stmt.(type) {
		case *ast.AlterTableStmt:
			err = c.alterTable(n)
		case *ast.CreateTableStmt:
			err = c.createTable(n)
		case *ast.DropTableStmt:
			err = c.dropTable(n)
		}
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

// TODO: This need to be rich error types
var ErrRelationNotFound = errors.New("relation not found")
var ErrSchemaNotFound = errors.New("schema not found")
var ErrColumnNotFound = errors.New("column not found")
var ErrColumnExists = errors.New("column already exists")

func (c *Catalog) getSchema(name string) (*Schema, error) {
	for i := range c.Schemas {
		if c.Schemas[i].Name == name {
			return c.Schemas[i], nil
		}
	}
	return nil, ErrSchemaNotFound
}

func (c *Catalog) getTable(name *ast.TableName) (*Schema, *Table, error) {
	ns := name.Schema
	if ns == "" {
		ns = c.DefaultSchema
	}
	var s *Schema
	for i := range c.Schemas {
		if c.Schemas[i].Name == ns {
			s = c.Schemas[i]
			break
		}
	}
	if s == nil {
		return nil, nil, ErrSchemaNotFound
	}
	t, _, err := s.getTable(name)
	if err != nil {
		return nil, nil, err
	}
	return s, t, nil
}

func (c *Catalog) alterTable(stmt *ast.AlterTableStmt) error {
	var implemented bool
	for _, item := range stmt.Cmds.Items {
		switch cmd := item.(type) {
		case *ast.AlterTableCmd:
			switch cmd.Subtype {
			case ast.AT_AddColumn:
				implemented = true
			case ast.AT_AlterColumnType:
				implemented = true
			case ast.AT_DropColumn:
				implemented = true
			case ast.AT_DropNotNull:
				implemented = true
			case ast.AT_SetNotNull:
				implemented = true
			}
		}
	}
	if !implemented {
		return nil
	}
	_, table, err := c.getTable(stmt.Table)
	if err != nil {
		return err
	}

	for _, cmd := range stmt.Cmds.Items {
		switch cmd := cmd.(type) {
		case *ast.AlterTableCmd:
			idx := -1

			// Lookup column names for column-related commands
			switch cmd.Subtype {
			case ast.AT_AlterColumnType,
				ast.AT_DropColumn,
				ast.AT_DropNotNull,
				ast.AT_SetNotNull:
				for i, c := range table.Columns {
					if c.Name == *cmd.Name {
						idx = i
						break
					}
				}
				if idx < 0 && !cmd.MissingOk {
					// return wrap(pg.ErrorColumnDoesNotExist(table.Name, *cmd.Name), raw.StmtLocation)
					return ErrColumnNotFound
				}
				// If a missing column is allowed, skip this command
				if idx < 0 && cmd.MissingOk {
					continue
				}
			}

			switch cmd.Subtype {

			case ast.AT_AddColumn:
				for _, c := range table.Columns {
					if c.Name == cmd.Def.Colname {
						// return wrap(pg.ErrorColumnAlreadyExists(table.Name, *d.Colname), d.Location)
						return ErrColumnExists
					}
				}
				table.Columns = append(table.Columns, &Column{
					Name:      cmd.Def.Colname,
					Type:      *cmd.Def.TypeName,
					IsNotNull: cmd.Def.IsNotNull,
				})

			case ast.AT_AlterColumnType:
				table.Columns[idx].Type = *cmd.Def.TypeName
				// table.Columns[idx].IsArray = isArray(d.TypeName)

			case ast.AT_DropColumn:
				table.Columns = append(table.Columns[:idx], table.Columns[idx+1:]...)

			case ast.AT_DropNotNull:
				table.Columns[idx].IsNotNull = false

			case ast.AT_SetNotNull:
				table.Columns[idx].IsNotNull = true

			}
		}
	}

	return nil
}

func (c *Catalog) createTable(stmt *ast.CreateTableStmt) error {
	ns := stmt.Name.Schema
	if ns == "" {
		ns = c.DefaultSchema
	}
	schema, err := c.getSchema(ns)
	if err != nil {
		return err
	}
	if _, _, err := schema.getTable(stmt.Name); err != nil {
		if !errors.Is(err, ErrRelationNotFound) {
			return err
		}
	} else if stmt.IfNotExists {
		return nil
	}
	tbl := Table{Rel: stmt.Name}
	for _, col := range stmt.Cols {
		tbl.Columns = append(tbl.Columns, &Column{
			Name:      col.Colname,
			Type:      *col.TypeName,
			IsNotNull: col.IsNotNull,
		})
	}
	schema.Tables = append(schema.Tables, &tbl)
	return nil
}

func (c *Catalog) dropTable(stmt *ast.DropTableStmt) error {
	for _, name := range stmt.Tables {
		ns := name.Schema
		if ns == "" {
			ns = c.DefaultSchema
		}
		schema, err := c.getSchema(ns)
		if errors.Is(err, ErrSchemaNotFound) && stmt.IfExists {
			continue
		} else if err != nil {
			return err
		}

		_, idx, err := schema.getTable(name)
		if errors.Is(err, ErrRelationNotFound) && stmt.IfExists {
			continue
		} else if err != nil {
			return err
		}

		schema.Tables = append(schema.Tables[:idx], schema.Tables[idx+1:]...)
	}
	return nil
}

type Catalog struct {
	Name    string
	Schemas []*Schema
	Comment string

	DefaultSchema string
}

type Schema struct {
	Name    string
	Tables  []*Table
	Comment string
}

func (s *Schema) getTable(rel *ast.TableName) (*Table, int, error) {
	for i := range s.Tables {
		if s.Tables[i].Rel.Name == rel.Name {
			return s.Tables[i], i, nil
		}
	}
	return nil, 0, ErrRelationNotFound
}

type Table struct {
	Rel     *ast.TableName
	Columns []*Column
	Comment string
}

// TODO: Should this just be ast Nodes?
type Column struct {
	Name      string
	Type      ast.TypeName
	IsNotNull bool
}
