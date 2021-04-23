/* eslint-disable ember-a11y-testing/a11y-audit-called */ // TODO
import { module, test } from 'qunit';
import { triggerEvent, visit } from '@ember/test-helpers';
import { setupApplicationTest } from 'ember-qunit';
import { setupMirage } from 'ember-cli-mirage/test-support';
import Layout from 'nomad-ui/tests/pages/layout';
import JobsList from 'nomad-ui/tests/pages/jobs/list';

module('Acceptance | search', function(hooks) {
  setupApplicationTest(hooks);
  setupMirage(hooks);

  test('pressing slash starts a search', async function(assert) {
    await visit('/');

    assert.notOk(Layout.navbar.search.field.isPresent);

    await triggerEvent('.page-layout', 'keydown', {
      keyCode: 191, // slash
    });

    assert.ok(Layout.navbar.search.field.isPresent);
  });

  test('pressing slash when an input element is focused does not start a search', async function(assert) {
    server.create('node');
    server.create('job');

    await visit('/');

    assert.notOk(Layout.navbar.search.field.isPresent);

    await JobsList.search.click();
    await JobsList.search.keydown({ keyCode: 191 });

    assert.notOk(Layout.navbar.search.field.isPresent);
  });
});
